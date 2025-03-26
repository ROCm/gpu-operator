/*
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the \"License\");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an \"AS IS\" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/internal/metricsexporter"
	log "github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
)

const ClusterTypeOpenShift = "openshift"
const ClusterTypeK8s = "kubernetes"
const HttpServerPort = "8084"

var kubectl = "kubectl"

type UserRequest struct {
	Command string `json:"command"`
}

func init() {
	c, err := exec.LookPath("kubectl")
	if err != nil {
		log.Fatalf("failed to find kubectl %v", err)
	}
	kubectl = c

	//Set logging properties
	log.SetReportCaller(true)
}

func CheckGpuLabel(rl v1.ResourceList) bool {
	s, ok := rl["amd.com/gpu"]
	if !ok {
		return false
	}

	if s.String() == "0" {
		return false
	}
	return true
}

func CheckDeploymentWithStandardKMMNFD(cl *kubernetes.Clientset, create bool) error {
	for _, d := range []struct {
		ns, name string
	}{
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-gpu-operator-charts-controller-manager"},
		{ns: "kmm-operator-system", name: "kmm-operator-controller"},
		{ns: "kmm-operator-system", name: "kmm-operator-webhook-server"},
		{ns: "node-feature-discovery", name: "nfd-master"},
	} {
		s, err := cl.AppsV1().Deployments(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if err == nil {
				return fmt.Errorf("Pod %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.Replicas == 0 || s.Status.ReadyReplicas != s.Status.Replicas {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}

	for _, d := range []struct {
		ns, name string
	}{
		{ns: "node-feature-discovery", name: "nfd-worker"},
	} {
		s, err := cl.AppsV1().DaemonSets(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if err == nil {
				return fmt.Errorf("Replica %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.DesiredNumberScheduled == 0 || s.Status.DesiredNumberScheduled != s.Status.NumberReady {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}
	return nil
}

func CheckOCDeploymentWithStandardKMMNFD(cl *kubernetes.Clientset, create bool) error {
	for _, d := range []struct {
		ns, name string
	}{
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-gpu-operator-charts-controller-manager"},
		{ns: "openshift-kmm", name: "kmm-operator-controller"},
		{ns: "openshift-kmm", name: "kmm-operator-webhook-server"},
		{ns: "openshift-nfd", name: "nfd-controller-manager"},
		{ns: "openshift-nfd", name: "nfd-master"},
	} {
		s, err := cl.AppsV1().Deployments(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if err == nil {
				return fmt.Errorf("Pod %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.Replicas == 0 || s.Status.ReadyReplicas != s.Status.Replicas {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}

	for _, d := range []struct {
		ns, name string
	}{
		{ns: "openshift-nfd", name: "nfd-worker"},
	} {
		s, err := cl.AppsV1().DaemonSets(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if err == nil {
				return fmt.Errorf("Replica %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.DesiredNumberScheduled == 0 || s.Status.DesiredNumberScheduled != s.Status.NumberReady {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}
	return nil
}

func CheckHelmOCDeployment(cl *kubernetes.Clientset, create bool) error {

	for _, d := range []struct {
		ns, name string
	}{
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-gpu-operator-charts-controller-manager"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-kmm-controller"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-kmm-webhook-server"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-nfd-controller-manager"},
		{ns: "kube-amd-gpu", name: "nfd-master"},
	} {
		s, err := cl.AppsV1().Deployments(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if err == nil {
				return fmt.Errorf("Pod %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.Replicas == 0 || s.Status.ReadyReplicas != s.Status.Replicas {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}

	for _, d := range []struct {
		ns, name string
	}{
		{ns: "kube-amd-gpu", name: "nfd-worker"},
	} {
		s, err := cl.AppsV1().DaemonSets(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if err == nil {
				return fmt.Errorf("Replica %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.DesiredNumberScheduled == 0 || s.Status.DesiredNumberScheduled != s.Status.NumberReady {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}
	return nil
}

func CheckHelmDeployment(cl *kubernetes.Clientset, ns string, create bool) error {
	for _, d := range []struct {
		ns, name string
	}{
		{ns: "cert-manager", name: "cert-manager"},
		{ns: "cert-manager", name: "cert-manager-cainjector"},
		{ns: "cert-manager", name: "cert-manager-webhook"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-gpu-operator-charts-controller-manager"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-kmm-controller"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-kmm-webhook-server"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-node-feature-discovery-gc"},
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-node-feature-discovery-master"},
	} {
		s, err := cl.AppsV1().Deployments(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if strings.Contains(d.name, "cert-manager") {
				continue
			}
			if err == nil {
				return fmt.Errorf("Pod %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.Replicas == 0 || s.Status.ReadyReplicas != s.Status.Replicas {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}

	for _, d := range []struct {
		ns, name string
	}{
		{ns: "kube-amd-gpu", name: "amd-gpu-operator-node-feature-discovery-worker"},
	} {
		s, err := cl.AppsV1().DaemonSets(d.ns).Get(context.TODO(), d.name, metav1.GetOptions{})
		if !create {
			if err == nil {
				return fmt.Errorf("Replica %v in namespace %v is not deleted yet", d.ns, d.name)
			}
		} else {
			if err != nil {
				return fmt.Errorf("failed to get %v/%v err %v", d.ns, d.name, err)
			}
			if s.Status.DesiredNumberScheduled == 0 || s.Status.DesiredNumberScheduled != s.Status.NumberReady {
				return fmt.Errorf("replicas not ready %v/%v status %v", d.ns, d.name, s.Status)
			}
		}
	}
	return nil
}

var rocmLabel = map[string]string{
	"e2e": "true",
}
var rocmDs = "e2e-rocm"

func DeployRocmPods(ctx context.Context, cl *kubernetes.Clientset,
	res *v1.ResourceRequirements) error {

	err := CreateDaemonsetVerify(ctx, cl, v1.NamespaceDefault, rocmDs,
		initContainerImage, rocmLabel, res)
	if err != nil {
		return fmt.Errorf("failed to create e2e pods %v", err)
	}

	if err := Retry(func() error {
		its, err := cl.CoreV1().Pods("").List(ctx, metav1.ListOptions{LabelSelector: kmmmodule.MapToLabelSelector(rocmLabel)})
		if err != nil {
			return fmt.Errorf("failed to list pods %v", err)
		}
		for _, p := range its.Items {
			for _, c := range p.Status.ContainerStatuses {
				if !c.Ready {
					return fmt.Errorf("pod %v/%v is not ready(%v)", p.Name, c.Name, c.Ready)

				}
			}
		}
		return nil
	}, time.Minute*5, time.Second*5); err != nil {
		return fmt.Errorf("pods not ready %v", err)
	}
	return nil
}

func ListRocmPods(ctx context.Context, cl *kubernetes.Clientset) ([]string, error) {
	pods := []string{}
	its, err := cl.CoreV1().Pods("").List(ctx, metav1.ListOptions{LabelSelector: kmmmodule.MapToLabelSelector(rocmLabel)})
	if err != nil {
		return pods, err
	}
	for _, p := range its.Items {
		pods = append(pods, p.Name)
	}
	return pods, err
}

func DelRocmPods(ctx context.Context, cl *kubernetes.Clientset) error {
	if err := DelDaemonset(cl, v1.NamespaceDefault, rocmDs); err != nil {
		return fmt.Errorf("failed to delete %v, %v", rocmDs, err)
	}
	if err := Retry(func() error {
		its, err := cl.CoreV1().Pods("").List(ctx, metav1.ListOptions{LabelSelector: kmmmodule.MapToLabelSelector(rocmLabel)})
		if err != nil {
			return fmt.Errorf("failed to list pods %v", err)
		}
		if len(its.Items) > 0 {
			return fmt.Errorf("pod %v exists", len(its.Items))
		}
		return nil
	}, time.Minute*5, time.Second*5); err != nil {
		return fmt.Errorf("pod(s) exist, %v", err)
	}
	return nil
}

func GetRocmInfo(name string) (string, error) {
	return ExecPodCmd("rocm-smi --alldevices -i | grep Name", v1.NamespaceDefault, name, "")
}

func ListGpuDrivers(name string) (string, error) {
	return ExecPodCmd("lsmod | grep amdgpu", v1.NamespaceDefault, name, "")
}

func GetGpuDriverVersion(name string) (string, error) {
	return ExecPodCmd("rocm-smi --showdriverversion | grep Driver", v1.NamespaceDefault, name, "")
}

func DeletePod(ctx context.Context, cl *kubernetes.Clientset, ns string,
	name string) error {
	rpodCli := cl.CoreV1().Pods(ns)
	return rpodCli.Delete(ctx, name, metav1.DeleteOptions{})
}

func CreateTLSSecret(ctx context.Context, cl *kubernetes.Clientset, name, ns string, crt, key []byte) error {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: map[string][]byte{
			"tls.crt": crt,
			"tls.key": key,
		},
		Type: v1.SecretTypeTLS,
	}
	_, err := cl.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	return err
}

func DeleteTLSSecret(ctx context.Context, cl *kubernetes.Clientset, name, ns string) error {
	return cl.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

func CreateDaemonsetVerify(ctx context.Context, cl *kubernetes.Clientset, ns string,
	name string, image string, matchLabels map[string]string,
	res *v1.ResourceRequirements) error {

	if res == nil {
		res = &v1.ResourceRequirements{
			Limits: v1.ResourceList{
				"amd.com/gpu": resource.MustParse("1"),
			},

			Requests: v1.ResourceList{
				"amd.com/gpu": resource.MustParse("1"),
			},
		}
	}

	dsCli := cl.AppsV1().DaemonSets(ns)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},

			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: v1.PodSpec{
					NodeSelector: map[string]string{"feature.node.kubernetes.io/amd-gpu": "true"},
					Containers: []v1.Container{
						{
							Name:      name,
							Image:     image,
							Command:   []string{"sh", "-c", "--"},
							Args:      []string{"sleep infinity"},
							Resources: *res,
						},
					},
				},
			},
		},
	}

	// Create Deployment
	_, err := dsCli.Create(context.TODO(), ds, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create daemonset %v", err)
	}

	// wait till it is ready, download time could vary
	return Retry(func() error {
		d, err := dsCli.Get(context.TODO(), ds.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get ds %v, %v", ds.Name, err)
		}
		if d.Status.NumberReady == 0 || d.Status.DesiredNumberScheduled != d.Status.NumberReady {
			return fmt.Errorf("ds %v not ready, %v", d.Name, d.Status)
		}
		return nil
	}, 10*time.Minute, time.Second*5)

}

func DelDaemonset(cl *kubernetes.Clientset, ns string, name string) error {
	dsCli := cl.AppsV1().DaemonSets(ns)
	deletePolicy := metav1.DeletePropagationForeground
	return dsCli.Delete(context.TODO(), name, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
}

func DevicePluginName(cfgName string) string {
	return cfgName + "-device-plugin"
}
func NodeLabellerName(cfgName string) string {
	return cfgName + "-node-labeller"
}
func NFDWorkerName(isOpenshift bool) string {
	if isOpenshift {
		return "nfd-worker"
	}
	return "amd-gpu-operator-node-feature-discovery-worker"
}

func ExecPodCmd(command string, ns string, name string, container string) (string, error) {
	var cmd *exec.Cmd
	if container != "" {
		cmd = exec.Command(kubectl, "exec", "-n", ns, name, "-c", container, "--", "sh", "-c", command)
	} else {
		cmd = exec.Command(kubectl, "exec", "-n", ns, name, "--", "sh", "-c", command)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func Retry(f func() error, timeout time.Duration, period time.Duration) error {
	timedout := time.After(timeout)
	tick := time.NewTicker(period)
	for {
		select {
		case <-timedout:
			return fmt.Errorf("timeout")
		case <-tick.C:
			if err := f(); err == nil {
				return nil
			}
		}
	}
}

func GetClusterType(cfg *rest.Config) string {
	if dc, err := discovery.NewDiscoveryClientForConfig(cfg); err == nil {
		if gplist, err := dc.ServerGroups(); err == nil {
			for _, gp := range gplist.Groups {
				if gp.Name == "route.openshift.io" {
					return ClusterTypeOpenShift
				}
			}
		}
	}
	return ClusterTypeK8s
}

func RunCommand(command string) {
	log.Infof("  %v", command)
	cmd := exec.Command("bash", "-c", command)
	output, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		log.Errorf("Command %v failed to start with error: %v", command, err)
		return
	}

	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		m := scanner.Text()
		log.Infof("    %v", m)
	}
	if err := cmd.Wait(); err != nil {
		log.Errorf("Coammand %v did not complete with error: %v", command, err)
	}
}

func RunCommandOnNode(ctx context.Context, cl *kubernetes.Clientset, nodeName, command string) (string, error) {

	nodeip, err := GetNodeIP(ctx, cl, nodeName)
	if err != nil {
		log.Errorf("node %s: %s get error: %v", nodeName, nodeip, err)
		return "", err
	}

	url := fmt.Sprintf("http://%s:%s/runcommand", nodeip, HttpServerPort)
	client := &http.Client{}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var userReq UserRequest
	userReq.Command = command
	reqJSON, _ := json.Marshal(userReq)
	reqBody := bytes.NewBuffer(reqJSON)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	log.Infof("resp status: %v error: %v", resp.Status, err)

	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("node health status: %v", resp.Status)
	}

	return string(body), nil
}

func GetWorkerNodes(cl *kubernetes.Clientset) []*v1.Node {
	ret := make([]*v1.Node, 0)

	labelSelector := labels.NewSelector()
	r, _ := labels.NewRequirement(
		"node-role.kubernetes.io/control-plane",
		selection.DoesNotExist,
		nil,
	)
	labelSelector = labelSelector.Add(*r)

	nodes, err := cl.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		log.Errorf("GetWorkerNodes error: %v", err)
		return ret
	}
	for i := 0; i < len(nodes.Items); i++ {
		node := &nodes.Items[i]
		ret = append(ret, node)
	}
	return ret
}

func GetAMDGpuWorker(cl *kubernetes.Clientset, isOpenshift bool) []v1.Node {
	ret := make([]v1.Node, 0)
	labelSelector := labels.NewSelector()
	r, _ := labels.NewRequirement(
		"feature.node.kubernetes.io/amd-gpu",
		selection.Equals,
		[]string{"true"},
	)
	labelSelector = labelSelector.Add(*r)

	nodes, err := cl.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		log.Errorf("GetWorkerNodes error: %v", err)
		return ret
	}
	for i := 0; i < len(nodes.Items); i++ {
		node := nodes.Items[i]
		ret = append(ret, node)
	}
	return ret
}

func GetNonAMDGpuWorker(cl *kubernetes.Clientset) []v1.Node {
	ret := make([]v1.Node, 0)

	labelSelector := labels.NewSelector()
	r, _ := labels.NewRequirement(
		"node-role.kubernetes.io/control-plane",
		selection.DoesNotExist,
		nil,
	)
	labelSelector = labelSelector.Add(*r)
	r, _ = labels.NewRequirement("gpu.vendor",
		selection.NotEquals,
		[]string{"amd"},
	)
	labelSelector = labelSelector.Add(*r)

	nodes, err := cl.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		log.Errorf("GetWorkerNodes error: %v", err)
		return ret
	}
	for i := 0; i < len(nodes.Items); i++ {
		node := nodes.Items[i]
		ret = append(ret, node)
	}
	return ret
}

func CreatePod(ctx context.Context, cl *kubernetes.Clientset, ns string,
	name string, image string, workerNodeName string) error {

	rpodCli := cl.CoreV1().Pods(ns)
	rpod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    name,
					Image:   image,
					Command: []string{"sh", "-c", "--"},
					Args:    []string{"sleep infinity"},
				},
			},
			NodeName: workerNodeName,
		},
	}

	// Create pod
	_, err := rpodCli.Create(context.TODO(), rpod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create pod %v", err)
	}
	return err
}

func DeployRocmPodsByNodeNames(ctx context.Context, cl *kubernetes.Clientset,
	workerNodeNames []string) error {

	for _, name := range workerNodeNames {

		err := CreatePod(ctx, cl, v1.NamespaceDefault,
			fmt.Sprintf("%s-%s", rocmDs, name), "rocm/tensorflow:latest", name)
		if err != nil {
			return fmt.Errorf("failed to create rocm as e2e pods %v", err)
		}
	}

	if err := Retry(func() error {

		for _, name := range workerNodeNames {
			its, err := cl.CoreV1().Pods("").List(ctx, metav1.ListOptions{
				FieldSelector: fmt.Sprintf("spec.nodeName=%s", name),
			})
			if err != nil {
				return fmt.Errorf("failed to get rocm e2e pods %v", err)
			}

			for _, p := range its.Items {
				for _, c := range p.Status.ContainerStatuses {
					if !c.Ready {
						return fmt.Errorf("pod %v/%v is not ready(%v)",
							p.Name, c.Name, c.Ready)
					}
				}
			}
		}
		return nil
	}, time.Minute*5, time.Second*5); err != nil {
		return fmt.Errorf("pods not ready %v", err)
	}
	return nil
}

func ListRocmPodsByNodeNames(ctx context.Context,
	workerNodeNames []string) []string {

	ret := make([]string, 0)
	for _, name := range workerNodeNames {
		ret = append(ret, fmt.Sprintf("%s-%s", rocmDs, name))
	}
	return ret
}

func DelRocmPodsByNodeNames(ctx context.Context, cl *kubernetes.Clientset,
	workerNodeNames []string) error {

	for _, name := range workerNodeNames {
		if err := DeletePod(ctx, cl, v1.NamespaceDefault,
			fmt.Sprintf("%s-%s", rocmDs, name)); err != nil {
			return fmt.Errorf("failed to delete %v, %v", rocmDs, err)
		}
	}

	if err := Retry(func() error {
		for _, node := range workerNodeNames {
			its, err := cl.CoreV1().Pods("").List(ctx, metav1.ListOptions{
				FieldSelector: fmt.Sprintf("spec.nodeName=%s", node),
			})
			if err != nil {
				return fmt.Errorf("failed to get rocm e2e pods %v", err)
			}
			for _, p := range its.Items {
				if p.Name == rocmDs {
					return fmt.Errorf("pod %v exists", len(its.Items))
				}
			}
		}
		return nil
	}, time.Minute*5, time.Second*5); err != nil {
		return fmt.Errorf("pod(s) exist, %v", err)
	}
	return nil

}

func GetAMDGPUCount(ctx context.Context, cl *kubernetes.Clientset, resourceType string) (map[string]int, error) {

	ret := make(map[string]int)
	// Get the list of nodes
	nodes, err := cl.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return ret, err
	}

	// Iterate over the nodes and count AMD GPUs
	for _, node := range nodes.Items {
		resourceKey := v1.ResourceName("amd.com/" + resourceType)
		if val, ok := node.Status.Capacity[resourceKey]; ok {
			num, err := strconv.ParseInt(val.String(), 10, 64)
			if err != nil {
				log.Infof("error: %v", err)
				continue
			}
			ret[node.Name] = int(num)
		}
	}
	return ret, nil
}

func VerifyROCMPODResourceCount(ctx context.Context, cl *kubernetes.Clientset,
	gpuReqCount int, resourceType string) error {

	its, err := cl.CoreV1().Pods("").List(ctx,
		metav1.ListOptions{
			LabelSelector: kmmmodule.MapToLabelSelector(rocmLabel),
		})
	if err != nil {
		return err
	}
	for _, p := range its.Items {
		for _, cntr := range p.Spec.Containers {
			if !strings.Contains(p.Name, rocmDs) {
				continue
			}

			resourceKey := v1.ResourceName("amd.com/" + resourceType)
			if gpu, ok := cntr.Resources.Requests[resourceKey]; ok {
				gpuAssignedCount := int(gpu.Value())
				if gpuReqCount < gpuAssignedCount {
					return fmt.Errorf("gpu requested %d got %d",
						gpuReqCount, gpuAssignedCount)
				}
			}
		}
	}
	return nil
}

func DeployNodeAppDaemonSet(cl *kubernetes.Clientset) error {
	hostPathDirectoryType := v1.HostPathDirectory
	ds := appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-nodeapp-ds",
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "e2e-nodeapp-ds",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"name": "e2e-nodeapp-ds",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:            "e2e-nodeapp-container",
							Image:           nodeAppImage,
							ImagePullPolicy: v1.PullAlways,
							Lifecycle: &v1.Lifecycle{
								PreStop: &v1.LifecycleHandler{
									Exec: &v1.ExecAction{
										Command: []string{"./docker-exitpoint.sh"},
									},
								},
							},
							Env: []v1.EnvVar{
								{
									Name: "NODE_IP",
									ValueFrom: &v1.EnvVarSource{
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "status.hostIP",
										},
									},
								},
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      "ssh-volume",
									MountPath: "/root/.ssh",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "ssh-volume",
							VolumeSource: v1.VolumeSource{
								HostPath: &v1.HostPathVolumeSource{
									Path: "/root/.ssh",
									Type: &hostPathDirectoryType,
								},
							},
						},
					},
				},
			},
		},
	}

	dsCli := cl.AppsV1().DaemonSets("default")
	_, reterr := dsCli.Create(context.TODO(), &ds, metav1.CreateOptions{})
	if reterr != nil {
		return fmt.Errorf("nodeapp create error: %v", reterr)
	}

	// wait till it is ready, download time could vary
	return Retry(func() error {
		d, err := dsCli.Get(context.TODO(), ds.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get ds %v, %v", ds.Name, err)
		}
		if d.Status.NumberReady == 0 || d.Status.DesiredNumberScheduled != d.Status.NumberReady {
			return fmt.Errorf("ds %v not ready, %v", d.Name, d.Status)
		}
		return nil
	}, 10*time.Minute, time.Second*5)
}

func GetClusterIP(clientset *kubernetes.Clientset, serviceName, namespace string) (string, error) {
	ctx := context.TODO()

	service, err := clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get service %s: %v", serviceName, err)
	}

	return service.Spec.ClusterIP, nil
}

func SplitYAML(data []byte) [][]byte {
	docs := strings.Split(string(data), "---")
	var result [][]byte
	for _, doc := range docs {
		trimmedDoc := strings.TrimSpace(doc)
		if trimmedDoc != "" {
			result = append(result, []byte(trimmedDoc))
		}
	}
	return result
}

func DeployResourcesFromFile(fileName string, cl *kubernetes.Clientset, create bool) error {
	fileName = "./yamls/config/" + fileName
	data, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file: %s", fileName)
	}

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()

	documents := SplitYAML(data)
	for _, doc := range documents {
		obj, _, err := decoder.Decode(doc, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to decode yaml %+v: %+v", doc, err)
		}

		switch resource := obj.(type) {
		case *v1.Namespace:
			if create {
				_, err = cl.CoreV1().Namespaces().Create(context.TODO(), resource, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("failed to create namespace %+v: %+v", resource, err)
				}
			} else {
				err = cl.CoreV1().Namespaces().Delete(context.TODO(), resource.Name, metav1.DeleteOptions{})
				if err != nil {
					return fmt.Errorf("failed to delete namespace %+v: %+v", resource, err)
				}
			}

		case *rbacv1.ClusterRole:
			if create {
				_, err = cl.RbacV1().ClusterRoles().Create(context.TODO(), resource, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("failed to create clusterrole %+v: %+v", resource, err)
				}
			} else {
				err = cl.RbacV1().ClusterRoles().Delete(context.TODO(), resource.Name, metav1.DeleteOptions{})
				if err != nil {
					return fmt.Errorf("failed to delete clusterrole %+v: %+v", resource, err)
				}
			}

		case *rbacv1.ClusterRoleBinding:
			if create {
				_, err = cl.RbacV1().ClusterRoleBindings().Create(context.TODO(), resource, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("failed to create clusterrole binding %+v: %+v", resource, err)
				}
			} else {
				err = cl.RbacV1().ClusterRoleBindings().Delete(context.TODO(), resource.Name, metav1.DeleteOptions{})
				if err != nil {
					return fmt.Errorf("failed to delete clusterrole binding %+v: %+v", resource, err)
				}
			}

		case *batchv1.Job:
			if create {
				_, err = cl.BatchV1().Jobs(resource.Namespace).Create(context.TODO(), resource, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("failed to create batch job %+v: %+v", resource, err)
				}
			} else {
				err = cl.BatchV1().Jobs(resource.Namespace).Delete(context.TODO(), resource.Name, metav1.DeleteOptions{})
				if err != nil {
					return fmt.Errorf("failed to delete batch job %+v: %+v", resource, err)
				}
			}

		default:
			return fmt.Errorf("unsupported resource type %+v", resource)
		}
	}
	return nil
}

func DeleteNodeAppDaemonSet(cl *kubernetes.Clientset) error {

	dsCli := cl.AppsV1().DaemonSets("default")
	reterr := dsCli.Delete(context.TODO(), "e2e-nodeapp-ds", metav1.DeleteOptions{})
	if reterr != nil {
		return fmt.Errorf("nodeapp delete error: %v", reterr)
	}
	return nil
}

func GetNodeIP(ctx context.Context, cl *kubernetes.Clientset,
	nodeName string) (string, error) {

	var nodeip string
	// Get the node object
	node, err := cl.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nodeip, fmt.Errorf("Error getting node: %v", err)
	}

	// Extract the IP address
	for _, address := range node.Status.Addresses {
		if address.Type == "InternalIP" {
			nodeip = address.Address
			break
		}
	}
	if nodeip == "" {
		return nodeip, fmt.Errorf("error getting ip of node: %v", err)
	}

	return nodeip, nil
}

func IsNodeHealthy(cl *kubernetes.Clientset, nodeip string) error {

	url := fmt.Sprintf("http://%s:%s/health", nodeip, HttpServerPort)
	client := &http.Client{}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	log.Infof("resp status: %v body: %v error: %v",
		resp.Status, string(body), err)

	if resp.StatusCode != 200 {
		return fmt.Errorf("node health status: %v", resp.Status)
	}
	if string(body) != "healthy" {
		return fmt.Errorf("node health body: %v", body)
	}

	return nil
}

func RebootNode(cl *kubernetes.Clientset, nodeip string) error {

	url := fmt.Sprintf("http://%s:%s/reboot", nodeip, HttpServerPort)
	client := &http.Client{}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Infof("resp status: %v body: %v error: %v",
		resp.Status, string(body), err)

	if resp.StatusCode != 200 {
		return fmt.Errorf("reboot failed response: %v", resp.Status)
	}
	return nil
}

func RebootNodeWithWait(ctx context.Context, cl *kubernetes.Clientset,
	nodeName string) error {

	nodeip, err := GetNodeIP(ctx, cl, nodeName)
	if err != nil || nodeip == "" {
		log.Errorf("node %s: %s get error: %v", nodeName, nodeip, err)
		return err
	}

	if err := RebootNode(cl, nodeip); err != nil {
		log.Errorf("node reboot error: %v", err)
		return err
	}

	if err := Retry(func() error {
		if err := IsNodeHealthy(cl, nodeip); err != nil {
			log.Errorf("node %s: %s health error: %v", nodeName, nodeip, err)
			return err
		}
		return nil
	}, time.Minute*10, time.Second*20); err != nil {
		return fmt.Errorf("node did not become healthy %v", err)
	}

	return nil
}

func GetJobLogs(clientset *kubernetes.Clientset, job *batchv1.Job) ([]string, error) {
	if job == nil {
		return nil, fmt.Errorf("Provide a valid job")
	}

	jobLogs := []string{}
	var logsBuffer bytes.Buffer
	podNames, err := GetPodNamesFromJob(clientset, job)
	if err != nil {
		return nil, err
	}
	for _, podName := range podNames {
		podLogOpts := v1.PodLogOptions{}
		req := clientset.CoreV1().Pods(job.Namespace).GetLogs(podName, &podLogOpts)

		logs, err := req.Stream(context.TODO())
		if err != nil {
			return nil, fmt.Errorf("failed to get logs for Pod %s: %v", podName, err)
		}
		defer logs.Close()

		_, err = io.Copy(&logsBuffer, logs)
		if err != nil {
			return nil, fmt.Errorf("failed to read logs for Pod %s: %v", podName, err)
		}

		jobLogs = append(jobLogs, fmt.Sprintf("Logs from Pod %s:\n%s\n", podName, logsBuffer.String()))
	}

	return jobLogs, nil
}

func GetPodNamesFromJob(clientset *kubernetes.Clientset, job *batchv1.Job) ([]string, error) {
	var podNames []string

	labelSelector := fmt.Sprintf("job-name=%s", job.Name)
	pods, err := clientset.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list Pods for Job %s: %v", job.Name, err)
	}

	for _, pod := range pods.Items {
		podNames = append(podNames, pod.Name)
	}

	return podNames, nil

}

func GetServiceEndpoints(clientset *kubernetes.Clientset, serviceName, namespace string) ([]string, error) {
	ctx := context.TODO()
	_, err := clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get service %s: %v", serviceName, err)
	}

	endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints for service %s: %v", serviceName, err)
	}

	var endpointIPs []string
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			endpointIPs = append(endpointIPs, address.IP)
		}
	}

	return endpointIPs, nil
}

func GenerateServiceAccountToken(clientset *kubernetes.Clientset, serviceAccountName, namespace string) (string, error) {
	ctx := context.TODO()

	seconds := int64(24 * 3600)
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &seconds,
		},
	}

	// Request a token for the service account
	tokenResp, err := clientset.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, serviceAccountName, tokenRequest, metav1.CreateOptions{})
	if err != nil || len(tokenResp.Status.Token) == 0 {
		return "", fmt.Errorf("failed to generate token for service account %s: %v tokenResp: %+v", serviceAccountName, err, tokenResp)
	}

	return tokenResp.Status.Token, nil
}

func CreateTempFile(fileName string, data []byte) (*os.File, error) {
	file, err := os.CreateTemp("", fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %v", err)
	}
	return file, nil
}

func DeleteTempFile(file *os.File) error {
	if file == nil {
		return fmt.Errorf("no valid file provided to delete")
	}
	return os.Remove(file.Name())
}

func CurlMetrics(endpointIPs []string, token string, port int, secure bool, caCert string) error {
	protocol := "https"
	if !secure {
		protocol = "http"
	}
	caCertStr := ""
	if len(caCert) > 0 {
		caCertStr = fmt.Sprintf("--cacert %s", caCert)
	} else {
		caCertStr = "-k"
	}
	for _, ip := range endpointIPs {
		cmd := fmt.Sprintf("curl -v -s %s -H \"Authorization: Bearer %s\" %s://%s:%d/metrics", caCertStr, token, protocol, ip, port)
		output, err := exec.Command("sh", "-c", cmd).Output()
		if err != nil {
			return fmt.Errorf("failed to curl endpoint %s: %v", ip, err)
		}
		if !strings.Contains(string(output), "gpu_id") {
			return fmt.Errorf("failed to fetch metrics, log: %s curl command: %s", string(output), cmd)
		}
	}
	return nil
}

func GetNodeIPs(clientset *kubernetes.Clientset) ([]string, error) {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes %v", err)
	}

	nodeIPs := []string{}
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == v1.NodeInternalIP || address.Type == v1.NodeExternalIP {
				nodeIPs = append(nodeIPs, address.Address)
			}
		}
	}
	return nodeIPs, nil
}

func GetNodeIPsForDaemonSet(clientset *kubernetes.Clientset, daemonSetName, namespace string) ([]string, error) {
	ctx := context.TODO()

	daemonSet, err := clientset.AppsV1().DaemonSets(namespace).Get(ctx, daemonSetName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get DaemonSet %s: %v", daemonSetName, err)
	}

	// Construct the label selector from the DaemonSet's selector
	labelSelector := metav1.FormatLabelSelector(daemonSet.Spec.Selector)

	// List Pods in the specified namespace with the matching label selector
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Pods for DaemonSet %s: %v", daemonSetName, err)
	}

	// Collect Node IPs from the Pods using HostIP
	var nodeIPs []string
	for _, pod := range pods.Items {
		nodeIPs = append(nodeIPs, pod.Status.HostIP)
	}

	return nodeIPs, nil
}

func RebootNodesWithWait(ctx context.Context, cl *kubernetes.Clientset, nodes []v1.Node) error {
	if len(nodes) == 0 {
		log.Errorf("No worker nodes provided for reboot")
		return nil
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(nodes))
	for _, node := range nodes {
		wg.Add(1)
		go func(node v1.Node) {
			defer wg.Done()

			if err := RebootNodeWithWait(ctx, cl, node.Name); err != nil {
				log.Errorf("Rebooting worker node %s failed with error: %v", node.Name, err)
				errCh <- err
				return
			}
			log.Infof("Worker node %s successfully rebooted!", node.Name)
		}(node)
	}

	wg.Wait()
	close(errCh)
	if len(errCh) > 0 {
		return <-errCh
	}

	return nil
}

func PatchOperatorControllerDeploymentWithCIENVFlag(cl *kubernetes.Clientset) error {
	patch := []map[string]interface{}{
		{
			"op":    "add",
			"path":  "/spec/template/spec/containers/0/env/-",
			"value": map[string]string{"name": "CI_ENV", "value": "true"},
		},
		{
			"op":    "add",
			"path":  "/spec/template/spec/containers/0/env/-",
			"value": map[string]string{"name": "INTERNAL_UBUNTU_BASE", "value": ubuntuBaseImage},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %v", err)
	}

	_, err = cl.AppsV1().Deployments("kube-amd-gpu").Patch(
		context.TODO(),
		"amd-gpu-operator-gpu-operator-charts-controller-manager",
		types.JSONPatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch gpu operator controller deployment: %v", err)
	}
	time.Sleep(60 * time.Second)
	return nil
}

func PatchKMMDeploymentWithCIENVFlag(cl *kubernetes.Clientset) error {
	patch := []map[string]interface{}{
		{
			"op":    "add",
			"path":  "/spec/template/spec/containers/0/env/-",
			"value": map[string]string{"name": "CI_ENV", "value": "true"},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %v", err)
	}

	_, err = cl.AppsV1().Deployments("kube-amd-gpu").Patch(
		context.TODO(),
		"amd-gpu-operator-kmm-controller",
		types.JSONPatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch KMM controller deployment: %v", err)
	}
	time.Sleep(60 * time.Second)
	return nil
}

func HandleNodesReboot(ctx context.Context, cl *kubernetes.Clientset, nodes []v1.Node) error {
	if len(nodes) == 0 {
		log.Errorf("No worker nodes provided for reboot")
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(nodes))

	for _, node := range nodes {
		wg.Add(1)
		go func(node v1.Node) {
			defer wg.Done()

			rebootPod := GetRebootPod(node.Name)

			// Delete the existing reboot pod if present
			if _, err := cl.CoreV1().Pods("kube-amd-gpu").Get(ctx, rebootPod.Name, metav1.GetOptions{}); err == nil {
				if err := cl.CoreV1().Pods("kube-amd-gpu").Delete(ctx, rebootPod.Name, metav1.DeleteOptions{}); err != nil {
					log.Errorf("Failed to delete existing reboot pod for node: %v, error: %v", node.Name, err)
					errCh <- err
					return
				}
			}

			// Create the reboot pod
			if _, err := cl.CoreV1().Pods("kube-amd-gpu").Create(ctx, rebootPod, metav1.CreateOptions{}); err != nil {
				log.Errorf("Failed to create reboot pod for node: %v, error: %v", node.Name, err)
				errCh <- err
				return
			}

			// Wait for the reboot pod to get spawned
			waitForRebootPod := func() {
				for i := uint(0); i < 300; _, i = <-time.NewTicker(2*time.Second).C, i+1 {
					if _, err := cl.CoreV1().Pods("kube-amd-gpu").Get(ctx, rebootPod.Name, metav1.GetOptions{}); err == nil {
						return
					}
				}
			}
			waitForRebootPod()

			// Delete the reboot pod after it has been created
			DeleteRebootPod(ctx, cl, node.Name, false)

			log.Infof("Worker node %s successfully rebooted!", node.Name)
		}(node)
	}

	wg.Wait()
	close(errCh)
	if len(errCh) > 0 {
		return <-errCh
	}

	return nil
}

func DeleteRebootPod(ctx context.Context, cl *kubernetes.Clientset, nodeName string, force bool) {
	rebootPod := GetRebootPod(nodeName)

	pod := &v1.Pod{}
	if _, err := cl.CoreV1().Pods("kube-amd-gpu").Get(ctx, rebootPod.Name, metav1.GetOptions{}); err != nil {
		return
	}

	if !force {
		// Wait (max 1 hour) until the pod is finished
		for i := uint(0); i < 60; _, i = <-time.NewTicker(10*time.Second).C, i+1 {
			if _, err := cl.CoreV1().Pods("kube-amd-gpu").Get(ctx, rebootPod.Name, metav1.GetOptions{}); err == nil {
				if len(pod.Status.ContainerStatuses) > 0 {
					containerStatus := pod.Status.ContainerStatuses[0]
					if containerStatus.State.Terminated != nil && !containerStatus.State.Terminated.FinishedAt.IsZero() {
						// Pod finished, delete it
						if err := cl.CoreV1().Pods("kube-amd-gpu").Delete(ctx, rebootPod.Name, metav1.DeleteOptions{}); err != nil {
							log.Errorf("Failed to delete reboot pod for node: %v, error: %v", nodeName, err)
						}
						return
					}
				}
			}
		}
	}

	// Force delete the pod if it's still present
	if err := cl.CoreV1().Pods("kube-amd-gpu").Delete(ctx, rebootPod.Name, metav1.DeleteOptions{}); err != nil {
		log.Errorf("Failed to delete reboot pod for node: %v, error: %v", nodeName, err)
	}
}

func GetRebootPod(nodeName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("amd-gpu-operator-%v-reboot-worker", nodeName),
			Namespace: "kube-amd-gpu",
		},
		Spec: v1.PodSpec{
			HostPID:       true,
			HostNetwork:   true,
			RestartPolicy: v1.RestartPolicyNever,
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []v1.Container{
				{
					Name:            "reboot-container",
					Image:           "docker.io/amdpsdo/gpu-operator-utils:latest",
					Command:         []string{"/nsenter", "--all", "--target=1", "--", "sudo", "reboot"},
					Stdin:           true,
					TTY:             true,
					SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
				},
			},
			Tolerations: []v1.Toleration{
				{
					Key:      "amd-gpu-driver-upgrade",
					Value:    "true",
					Operator: v1.TolerationOpEqual,
					Effect:   v1.TaintEffectNoSchedule,
				},
			},
		},
	}
}

func CreateDaemonset(cl *kubernetes.Clientset, ns string, name string, image string, matchLabels map[string]string, res *v1.ResourceRequirements) error {

	if res == nil {
		res = &v1.ResourceRequirements{
			Limits: v1.ResourceList{
				"amd.com/gpu": resource.MustParse("1"),
			},

			Requests: v1.ResourceList{
				"amd.com/gpu": resource.MustParse("1"),
			},
		}
	}

	dsCli := cl.AppsV1().DaemonSets(ns)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},

			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: v1.PodSpec{
					NodeSelector: map[string]string{"feature.node.kubernetes.io/amd-gpu": "true"},
					Containers: []v1.Container{
						{
							Name:      name,
							Image:     image,
							Command:   []string{"sh", "-c", "--"},
							Args:      []string{"sleep infinity"},
							Resources: *res,
						},
					},
				},
			},
		},
	}

	// Create Deployment
	_, err := dsCli.Create(context.TODO(), ds, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create daemonset %v", err)
	}
	return nil
}

func SetGPUHealthOnNode(cl *kubernetes.Clientset, ns, gpuid, health, nodeName string) error {
	podFieldSelector := ""
	if nodeName != "" {
		podFieldSelector = fmt.Sprintf("spec.nodeName=%s", nodeName)
	}
	pods, err := cl.CoreV1().Pods(ns).List(context.TODO(),
		metav1.ListOptions{LabelSelector: kmmmodule.MapToLabelSelector(
			map[string]string{"app.kubernetes.io/name": metricsexporter.ExporterName}),
			FieldSelector: podFieldSelector})
	if err != nil {
		return err
	}
	var cmd1 string
	if health == "healthy" {
		cmd1 = fmt.Sprintf(`echo "{\"ID\": \"%s\",\"Fields\": [\"GPU_ECC_UNCORRECT_SEM\",\"GPU_ECC_UNCORRECT_FUSE\"],\"Counts\" : [0, 0]}" > /tmp/ecc.json`, gpuid)
	} else {
		cmd1 = fmt.Sprintf(`echo "{\"ID\": \"%s\",\"Fields\": [\"GPU_ECC_UNCORRECT_SEM\",\"GPU_ECC_UNCORRECT_FUSE\"],\"Counts\" : [1, 2]}" > /tmp/ecc.json`, gpuid)

	}
	_, err = ExecPodCmd(cmd1, ns, pods.Items[0].Name, metricsexporter.ExporterName+"-container")
	if err != nil {
		return err
	}
	cmd2 := "metricsclient -ecc-file-path /tmp/ecc.json"
	_, err = ExecPodCmd(cmd2, ns, pods.Items[0].Name, metricsexporter.ExporterName+"-container")
	return err
}

func IsJSONParsable(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

func AddNodeLabel(cl *kubernetes.Clientset, nodeName string, key string, value string) error {

	node, err := cl.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	// Add a label to the node
	node.Labels[key] = value

	// Update the node object with the new label
	_, err = cl.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil {
		panic(err.Error())
	}

	log.Infof("Label added successfully")
	if err != nil {
		return fmt.Errorf("failed to add node label to node: %v", err)
	}
	return nil
}

func DeleteNodeLabel(cl *kubernetes.Clientset, nodeName string, key string) error {

	node, err := cl.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	// Remove a label to the node
	delete(node.Labels, key)

	// Update the node object with the new label
	_, err = cl.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil {
		panic(err.Error())
	}

	log.Infof("Label removed successfully")
	if err != nil {
		return fmt.Errorf("failed to remove node label to node: %v", err)
	}
	return nil
}

func NodeTaint(cl *kubernetes.Clientset, nodeName string) error {
	log.Print("Handle Node Taint")
	node, err := cl.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	// Add a taint to the node
	taint := v1.Taint{
		Key:    "dcm",
		Value:  "up",
		Effect: v1.TaintEffectNoExecute,
	}
	node.Spec.Taints = append(node.Spec.Taints, taint)

	// Update the node object with the new taint
	_, err = cl.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	log.Infof("Updated node %q with taint.\n", nodeName)
	return nil
}
func CreateOpaqueSecret(ctx context.Context, cl *kubernetes.Clientset, name, ns string, keys map[string]string) error {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		StringData: keys,
		Type:       v1.SecretTypeOpaque,
	}
	_, err := cl.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	return err
}

func DeleteOpaqueSecret(ctx context.Context, cl *kubernetes.Clientset, name, ns string) {
	err := cl.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		log.Errorf("Unable to delete secret. Error: %v", err)
	}
}

func CreateMinioService(ctx context.Context, cl *kubernetes.Clientset, ns, hostName string) error {
	hostPathDirectory := v1.HostPathDirectoryOrCreate
	minioPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minio",
			Namespace: ns,
			Labels:    map[string]string{"app": "minio"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    "minio",
					Image:   minioImage,
					Command: []string{"/bin/bash", "-c"},
					Args:    []string{"minio server /data --console-address :9090"},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "localvolume",
							MountPath: "/data",
						},
					},
				},
			},
			NodeSelector: map[string]string{"kubernetes.io/hostname": hostName},
			Volumes: []v1.Volume{
				{
					Name: "localvolume",
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/data",
							Type: &hostPathDirectory,
						},
					},
				},
			},
		},
	}
	minioService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minio",
			Namespace: ns,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:     "console",
					Port:     9090,
					NodePort: 31250,
				},
				{
					Name:     "s3",
					Port:     9000,
					NodePort: 31260,
				},
			},
			Selector: map[string]string{"app": "minio"},
			Type:     v1.ServiceTypeNodePort,
		},
	}
	_, err := cl.CoreV1().Pods(ns).Create(ctx, minioPod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	_, err = cl.CoreV1().Services(ns).Create(ctx, minioService, metav1.CreateOptions{})
	return err
}

func SetupAccessKeysOnMinioServer(ns, pod, container, accessKey, secretKey string) {
	cmd := fmt.Sprintf("mc alias set local http://localhost:9000 minioadmin minioadmin && mc admin accesskey create local/ minioadmin --access-key %s --secret-key %s", accessKey, secretKey)
	_, err := ExecPodCmd(cmd, ns, pod, container)
	if err != nil {
		log.Errorf("Access key cmd errored. Error: %v", err)
	}
}

func DeleteMinioService(ctx context.Context, cl *kubernetes.Clientset, ns string) {
	err := cl.CoreV1().Pods(ns).Delete(ctx, "minio", metav1.DeleteOptions{})
	if err != nil {
		log.Errorf("Failed to delete minio pod. Error: %v", err)
	}
	err = cl.CoreV1().Services(ns).Delete(ctx, "minio", metav1.DeleteOptions{})
	if err != nil {
		log.Errorf("Failed to delete minio service. Error: %v", err)
	}
}
