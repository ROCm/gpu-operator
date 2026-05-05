/**
# Copyright (c) Advanced Micro Devices, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the \"License\");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an \"AS IS\" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package clients

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/prometheus/common/expfmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

// moduleGVR is the GroupVersionResource for KMM Module CRs.
var moduleGVR = schema.GroupVersionResource{
	Group:    "kmm.sigs.x-k8s.io",
	Version:  "v1beta1",
	Resource: "modules",
}

// deviceconfigGVR is the GroupVersionResource for AMD DeviceConfig CRs.
var deviceconfigGVR = schema.GroupVersionResource{
	Group:    "amd.com",
	Version:  "v1alpha1",
	Resource: "deviceconfigs",
}

type K8sClient struct {
	client  *kubernetes.Clientset
	dynamic dynamic.Interface
}

func NewK8sClient(config *restclient.Config) (*K8sClient, error) {
	k8sc := K8sClient{}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	k8sc.client = cs
	dc, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	k8sc.dynamic = dc
	return &k8sc, nil
}

// clearFinalizers removes all finalizers from every CR of the given GVR in namespace.
// This unblocks namespace deletion when the owning controller is no longer running.
func (k *K8sClient) clearFinalizers(ctx context.Context, gvr schema.GroupVersionResource, namespace string) {
	list, err := k.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("clearFinalizers %s: list: %v", gvr.Resource, err)
		}
		return
	}
	for _, item := range list.Items {
		name := item.GetName()
		if len(item.GetFinalizers()) == 0 {
			continue
		}
		item.SetFinalizers(nil)
		if _, err := k.dynamic.Resource(gvr).Namespace(namespace).Update(ctx, &item, metav1.UpdateOptions{}); err != nil {
			log.Printf("clearFinalizers %s/%s: clear: %v", gvr.Resource, name, err)
		} else {
			log.Printf("clearFinalizers %s/%s: finalizers cleared", gvr.Resource, name)
		}
	}
}

// deleteKMMWebhook removes the KMM ValidatingWebhookConfiguration so that Module CR
// finalizers can be stripped without the webhook intercepting the PATCH request.
func (k *K8sClient) deleteKMMWebhook(ctx context.Context) {
	// The webhook name is chart-dependent; delete any that reference kmm.
	list, err := k.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, wh := range list.Items {
		for _, w := range wh.Webhooks {
			if strings.Contains(w.Name, "kmm") {
				log.Printf("deleteKMMWebhook: deleting ValidatingWebhookConfiguration %s", wh.Name)
				_ = k.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, wh.Name, metav1.DeleteOptions{})
				break
			}
		}
	}
}

// crdGVR is the GroupVersionResource for CustomResourceDefinitions (cluster-scoped).
var crdGVR = schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
}

// CleanupClusterScopedResources removes cluster-scoped resources left behind by a previous
// GPU Operator helm release. These persist after namespace deletion and block a fresh install
// with a different release name.
func (k *K8sClient) CleanupClusterScopedResources(ctx context.Context, oldReleaseName string) {
	// Patch helm ownership annotation on CRDs to the new release name so helm install adopts them.
	crdNames := []string{
		"modules.kmm.sigs.x-k8s.io",
		"nodemodulesconfigs.kmm.sigs.x-k8s.io",
		"preflightvalidations.kmm.sigs.x-k8s.io",
		"deviceconfigs.amd.com",
		"remediationworkflowstatuses.amd.com",
	}
	for _, name := range crdNames {
		_ = k.dynamic.Resource(crdGVR).Delete(ctx, name, metav1.DeleteOptions{})
	}

	// DeviceClass — try both v1 (k8s 1.32+) and v1beta1 (older clusters).
	for _, dcVersion := range []string{"v1", "v1beta1"} {
		deviceClassGVR := schema.GroupVersionResource{Group: "resource.k8s.io", Version: dcVersion, Resource: "deviceclasses"}
		dcs, err := k.dynamic.Resource(deviceClassGVR).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/managed-by=Helm",
		})
		if err != nil {
			continue
		}
		for _, dc := range dcs.Items {
			ann := dc.GetAnnotations()
			if ann["meta.helm.sh/release-name"] == oldReleaseName {
				log.Printf("CleanupClusterScopedResources: deleting DeviceClass %s (resource.k8s.io/%s)", dc.GetName(), dcVersion)
				_ = k.dynamic.Resource(deviceClassGVR).Delete(ctx, dc.GetName(), metav1.DeleteOptions{})
			}
		}
	}

	// ClusterRoles and ClusterRoleBindings.
	crList, _ := k.client.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=Helm",
	})
	if crList != nil {
		for _, cr := range crList.Items {
			if cr.Annotations["meta.helm.sh/release-name"] == oldReleaseName {
				log.Printf("CleanupClusterScopedResources: deleting ClusterRole %s", cr.Name)
				_ = k.client.RbacV1().ClusterRoles().Delete(ctx, cr.Name, metav1.DeleteOptions{})
			}
		}
	}
	crbList, _ := k.client.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=Helm",
	})
	if crbList != nil {
		for _, crb := range crbList.Items {
			if crb.Annotations["meta.helm.sh/release-name"] == oldReleaseName {
				log.Printf("CleanupClusterScopedResources: deleting ClusterRoleBinding %s", crb.Name)
				_ = k.client.RbacV1().ClusterRoleBindings().Delete(ctx, crb.Name, metav1.DeleteOptions{})
			}
		}
	}

	// PriorityClass.
	pcList, _ := k.client.SchedulingV1().PriorityClasses().List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=Helm",
	})
	if pcList != nil {
		for _, pc := range pcList.Items {
			if pc.Annotations["meta.helm.sh/release-name"] == oldReleaseName {
				log.Printf("CleanupClusterScopedResources: deleting PriorityClass %s", pc.Name)
				_ = k.client.SchedulingV1().PriorityClasses().Delete(ctx, pc.Name, metav1.DeleteOptions{})
			}
		}
	}

	// Validating and mutating webhook configurations.
	for _, whGVR := range []schema.GroupVersionResource{
		{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"},
		{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"},
	} {
		list, err := k.dynamic.Resource(whGVR).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/managed-by=Helm",
		})
		if err != nil {
			continue
		}
		for _, obj := range list.Items {
			ann := obj.GetAnnotations()
			if ann["meta.helm.sh/release-name"] == oldReleaseName {
				log.Printf("CleanupClusterScopedResources: deleting webhook %s %s", whGVR.Resource, obj.GetName())
				_ = k.dynamic.Resource(whGVR).Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
			}
		}
	}

	// NodeFeatureRule (nfd.k8s-sigs.io/v1) and other custom cluster-scoped resources.
	for _, gvr := range []schema.GroupVersionResource{
		{Group: "nfd.k8s-sigs.io", Version: "v1", Resource: "nodefeaturerules"},
		{Group: "argoproj.io", Version: "v1alpha1", Resource: "clusterworkflowtemplates"},
	} {
		list, err := k.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/managed-by=Helm",
		})
		if err != nil {
			continue
		}
		for _, obj := range list.Items {
			ann := obj.GetAnnotations()
			if ann["meta.helm.sh/release-name"] == oldReleaseName {
				log.Printf("CleanupClusterScopedResources: deleting %s/%s %s", gvr.Group, gvr.Resource, obj.GetName())
				_ = k.dynamic.Resource(gvr).Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
			}
		}
	}
}

// releaseGPUOperatorCRDs removes the helm release ownership annotations from GPU Operator CRDs
// so that a fresh install with a new release name can adopt them without conflicts.
func (k *K8sClient) releaseGPUOperatorCRDs(ctx context.Context, newReleaseName, newNamespace string) {
	names := []string{
		"modules.kmm.sigs.x-k8s.io",
		"nodemodulesconfigs.kmm.sigs.x-k8s.io",
		"preflightvalidations.kmm.sigs.x-k8s.io",
		"deviceconfigs.amd.com",
		"remediationworkflowstatuses.amd.com",
	}
	for _, name := range names {
		obj, err := k.dynamic.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			log.Printf("releaseGPUOperatorCRDs: get %s: %v", name, err)
			continue
		}
		ann := obj.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}
		ann["meta.helm.sh/release-name"] = newReleaseName
		ann["meta.helm.sh/release-namespace"] = newNamespace
		obj.SetAnnotations(ann)
		if _, err := k.dynamic.Resource(crdGVR).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
			log.Printf("releaseGPUOperatorCRDs: update %s: %v", name, err)
		} else {
			log.Printf("releaseGPUOperatorCRDs: re-annotated %s → %s/%s", name, newNamespace, newReleaseName)
		}
	}
}

// DeleteNamespaceAndWait removes GPU Operator CRs and their finalizers, deletes the namespace,
// and polls until it is fully gone.
func (k *K8sClient) DeleteNamespaceAndWait(ctx context.Context, namespace, _ string, timeout time.Duration) error {
	// Remove the KMM validating webhook first so Module finalizer patches succeed.
	k.deleteKMMWebhook(ctx)
	// Strip finalizers from both CRs so the namespace can terminate cleanly.
	k.clearFinalizers(ctx, moduleGVR, namespace)
	k.clearFinalizers(ctx, deviceconfigGVR, namespace)

	if err := k.client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{}); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// Poll until the namespace is gone.
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := k.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		log.Printf("DeleteNamespaceAndWait: namespace %s still terminating…", namespace)
		return false, nil
	})
}

func (k *K8sClient) NamespaceExists(ctx context.Context, namespace string) bool {
	_, err := k.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	return err == nil
}

func (k *K8sClient) CreateNamespace(ctx context.Context, namespace string) error {
	namespaceObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
		Status: corev1.NamespaceStatus{},
	}
	_, err := k.client.CoreV1().Namespaces().Create(ctx, namespaceObj, metav1.CreateOptions{})
	return err
}

func (k *K8sClient) DeleteNamespace(ctx context.Context, namespace string) error {
	return k.client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
}

func (k *K8sClient) GetPodsByLabel(ctx context.Context, namespace string, labelMap map[string]string) ([]corev1.Pod, error) {
	podList, err := k.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelMap).String(),
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func (k *K8sClient) GetNodesByLabel(ctx context.Context, labelMap map[string]string) ([]corev1.Node, error) {
	nodeList, err := k.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelMap).String(),
	})
	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func (k *K8sClient) GetServiceByLabel(ctx context.Context, namespace string, labelMap map[string]string) ([]corev1.Service, error) {
	nodeList, err := k.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelMap).String(),
	})
	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func (k *K8sClient) ValidatePod(ctx context.Context, namespace, podName string) error {
	pod, err := k.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unexpected error getting pod %s; err: %w", podName, err)
	}

	for _, c := range pod.Status.ContainerStatuses {
		if c.State.Waiting != nil && c.State.Waiting.Reason == "CrashLoopBackOff" {
			return fmt.Errorf("pod %s in namespace %s is in CrashLoopBackOff", pod.Name, pod.Namespace)
		}
	}

	return nil
}

func (k *K8sClient) GetMetricsCmdFromPod(ctx context.Context, rc *restclient.Config, pod *corev1.Pod) (labels []string, fields []string, err error) {
	if pod == nil {
		return nil, nil, fmt.Errorf("invalid pod")
	}
	req := k.client.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	cmd := "curl -s localhost:5000/metrics"
	req.VersionedParams(&corev1.PodExecOptions{
		Command: []string{"/bin/sh", "-c", cmd},
		Stdin:   false,
		Stdout:  true,
		Stderr:  false,
		TTY:     false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(rc, "POST", req.URL())
	if err != nil {
		return nil, nil, err
	}

	buf := &bytes.Buffer{}
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: buf,
		Tty:    false,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w failed executing command %s on %v/%v", err, cmd, pod.Namespace, pod.Name)
	}
	//log.Printf("\nbuf : %v\n", buf.String())
	p := expfmt.TextParser{}
	m, err := p.TextToMetricFamilies(buf)
	if err != nil {
		return nil, nil, fmt.Errorf("%w failed parsing to metrics", err)
	}
	for _, f := range m {
		fields = append(fields, *f.Name)
		for _, km := range f.Metric {
			if len(labels) != 0 {
				continue
			}
			for _, lp := range km.GetLabel() {
				labels = append(labels, *lp.Name)
			}
		}

	}
	return
}

func (k *K8sClient) CreateConfigMap(ctx context.Context, namespace string, name string, json string) error {
	mcfgMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"config.json": json,
		},
	}

	_, err := k.client.CoreV1().ConfigMaps(namespace).Create(ctx, mcfgMap, metav1.CreateOptions{})
	return err
}

func (k *K8sClient) UpdateConfigMap(ctx context.Context, namespace string, name string, json string) error {
	existing, err := k.client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	existing.Data["config.json"] = json
	_, err = k.client.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (k *K8sClient) DeleteConfigMap(ctx context.Context, namespace string, name string) error {
	return k.client.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// WaitForNodeLabel polls until at least one node has the given label key=value, or timeout expires.
func (k *K8sClient) WaitForNodeLabel(ctx context.Context, labelKey, labelValue string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 10*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		nodeList, err := k.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{labelKey: labelValue}).String(),
		})
		if err != nil {
			return false, err
		}
		if len(nodeList.Items) > 0 {
			log.Printf("WaitForNodeLabel: %d node(s) have %s=%s", len(nodeList.Items), labelKey, labelValue)
			return true, nil
		}
		return false, nil
	})
}

// WaitForDaemonSetReady polls until all desired pods of a DaemonSet are ready, or timeout expires.
func (k *K8sClient) WaitForDaemonSetReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 10*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		ds, err := k.client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled {
			return true, nil
		}
		return false, nil
	})
}

// GetNodeAllocatableGPUs returns the number of amd.com/gpu allocatable resources on a node.
// Returns -1 if the resource is not present.
func (k *K8sClient) GetNodeAllocatableGPUs(ctx context.Context, nodeName string) (int64, error) {
	node, err := k.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	qty, ok := node.Status.Allocatable["amd.com/gpu"]
	if !ok {
		return -1, nil
	}
	return qty.Value(), nil
}

// CreatePod creates a Pod in the given namespace and returns any error.
func (k *K8sClient) CreatePod(ctx context.Context, namespace string, pod *corev1.Pod) error {
	_, err := k.client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	return err
}

// WaitForPodSucceeded polls until the pod reaches Succeeded phase or timeout expires.
func (k *K8sClient) WaitForPodSucceeded(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 10*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := k.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed:
			return false, fmt.Errorf("pod %s failed", podName)
		}
		return false, nil
	})
}

// DeletePod deletes a pod by name.
func (k *K8sClient) DeletePod(ctx context.Context, namespace, name string) error {
	return k.client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// WaitForPodDeleted polls until the named pod no longer exists (or timeout).
func (k *K8sClient) WaitForPodDeleted(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := k.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
}

// GetPodLogs returns the logs for a pod's first container.
func (k *K8sClient) GetPodLogs(ctx context.Context, namespace, podName string) (string, error) {
	req := k.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	result := req.Do(ctx)
	raw, err := result.Raw()
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GetNodeNames returns all node names in the cluster.
func (k *K8sClient) GetNodeNames(ctx context.Context) ([]string, error) {
	nodeList, err := k.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(nodeList.Items))
	for _, n := range nodeList.Items {
		names = append(names, n.Name)
	}
	return names, nil
}

func (k *K8sClient) ExecCmdOnPod(ctx context.Context, rc *restclient.Config, pod *corev1.Pod, container, execCmd string) (string, error) {
	if pod == nil {
		return "", fmt.Errorf("No pod specified")
	}
	req := k.client.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).Namespace(pod.Namespace).SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{"/bin/sh", "-c", execCmd},
		Stdin:     false,
		Stdout:    true,
		Stderr:    false,
		TTY:       false,
	}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(rc, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create command executor. Error:%v", err)
	}
	buf := &bytes.Buffer{}
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: buf,
		Tty:    false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to run command on pod %v. Error:%v", pod.Name, err)
	}

	return buf.String(), nil
}

// DeleteCertManagerKubeSystemRoles removes cert-manager Roles from kube-system that may
// have been left without helm ownership metadata after a previous run's cleanup. Without
// proper labels/annotations, helm cannot adopt them and fails with "cannot be imported".
func (k *K8sClient) DeleteCertManagerKubeSystemRoles(ctx context.Context) {
	certMgrRoles := []string{
		"cert-manager-cainjector:leaderelection",
		"cert-manager:leaderelection",
		"cert-manager-webhook:dynamic-serving",
	}
	for _, roleName := range certMgrRoles {
		err := k.client.RbacV1().Roles("kube-system").Delete(ctx, roleName, metav1.DeleteOptions{})
		if err != nil {
			log.Printf("DeleteCertManagerKubeSystemRoles: %s: %v", roleName, err)
		} else {
			log.Printf("DeleteCertManagerKubeSystemRoles: deleted kube-system/Role/%s", roleName)
		}
	}
	// Also delete RoleBindings
	certMgrRoleBindings := []string{
		"cert-manager-cainjector:leaderelection",
		"cert-manager:leaderelection",
		"cert-manager-webhook:dynamic-serving",
	}
	for _, rbName := range certMgrRoleBindings {
		err := k.client.RbacV1().RoleBindings("kube-system").Delete(ctx, rbName, metav1.DeleteOptions{})
		if err != nil {
			log.Printf("DeleteCertManagerKubeSystemRoles: rolebinding %s: %v", rbName, err)
		} else {
			log.Printf("DeleteCertManagerKubeSystemRoles: deleted kube-system/RoleBinding/%s", rbName)
		}
	}
}
