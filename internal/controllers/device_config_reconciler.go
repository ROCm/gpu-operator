/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ROCm/gpu-operator/internal/configmanager"
	"github.com/ROCm/gpu-operator/internal/metricsexporter"
	"github.com/ROCm/gpu-operator/internal/testrunner"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	"github.com/rh-ecosystem-edge/kernel-module-management/pkg/labels"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/conditions"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/internal/nodelabeller"
	"github.com/ROCm/gpu-operator/internal/validator"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	event "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	DeviceConfigReconcilerName = "DriverAndPluginReconciler"
	deviceConfigFinalizer      = "amd.node.kubernetes.io/deviceconfig-finalizer"
	testRunnerNodeLabelPrefix  = "testrunner.amd.com"
)

// ModuleReconciler reconciles a Module object
type DeviceConfigReconciler struct {
	once            sync.Once
	initErr         error
	helper          deviceConfigReconcilerHelperAPI
	podEventHandler podEventHandlerAPI
}

func NewDeviceConfigReconciler(
	k8sConfig *rest.Config,
	client client.Client,
	kmmHandler kmmmodule.KMMModuleAPI,
	nlHandler nodelabeller.NodeLabeller,
	metricsHandler metricsexporter.MetricsExporter,
	testrunnerHandler testrunner.TestRunner,
	configmanagerHandler configmanager.ConfigManager) *DeviceConfigReconciler {
	upgradeMgrHandler := newUpgradeMgrHandler(client, k8sConfig)
	helper := newDeviceConfigReconcilerHelper(client, kmmHandler, nlHandler, upgradeMgrHandler, metricsHandler, testrunnerHandler, configmanagerHandler)
	podEventHandler := newPodEventHandler(client)
	return &DeviceConfigReconciler{
		helper:          helper,
		podEventHandler: podEventHandler,
	}
}

// SetupWithManager sets up the controller with the Manager.
//  1. Owns() will tell the manager that if any Module or Daemonset object or their status got updated
//     the DeviceConfig object in their ref field need to be reconciled
//  2. findDeviceConfigsForNMC: when a NMC changed, only trigger reconcile for related DeviceConfig
func (r *DeviceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&amdv1alpha1.DeviceConfig{}).
		Owns(&kmmv1beta1.Module{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&v1.Service{}).
		Named(DeviceConfigReconcilerName).
		Watches( // watch NMC for updating the DeviceConfigs CR status
			&kmmv1beta1.NodeModulesConfig{},
			handler.EnqueueRequestsFromMapFunc(r.helper.findDeviceConfigsForNMC),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(&v1.Secret{}, // watch for KMM build/sign/install related secrets
			handler.EnqueueRequestsFromMapFunc(r.helper.findDeviceConfigsForSecret),
			builder.WithPredicates(
				predicate.Funcs{
					CreateFunc: func(e event.CreateEvent) bool {
						return true
					},
					UpdateFunc: func(e event.UpdateEvent) bool {
						return true
					},
					DeleteFunc: func(e event.DeleteEvent) bool {
						return true
					},
				},
			),
		).
		Watches(&v1.Node{}, // watch for Node resource to get latest kernel mapping for KMM CR
			handler.EnqueueRequestsFromMapFunc(r.helper.findDeviceConfigsWithKMM),
			builder.WithPredicates(NodeKernelVersionPredicate{}),
		).
		Watches( // watch for KMM builder pod event to auto-clean unknown status builder pod
			&v1.Pod{},
			r.podEventHandler,
			builder.WithPredicates(PodLabelPredicate{}), // only watch for event from kmm builder pod
		).Complete(r)
}

func (r *DeviceConfigReconciler) init(ctx context.Context) {
	// List existing Device Configs
	deviceConfigList, err := r.helper.listDeviceConfigs(ctx)
	if err != nil {
		r.initErr = err
		return
	}
	r.initErr = r.helper.buildNodeAssignments(deviceConfigList)
}

//+kubebuilder:rbac:groups=amd.com,resources=deviceconfigs,verbs=get;list;watch;create;patch;update
//+kubebuilder:rbac:groups=amd.com,resources=deviceconfigs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=amd.com,resources=deviceconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules,verbs=get;list;watch;create;patch;update;delete
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules/finalizers,verbs=get;update;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs/finalizers,verbs=get;update;watch
//+kubebuilder:rbac:groups=nfd.openshift.io,resources=nodefeaturediscoveries,verbs=list;get;delete
//+kubebuilder:rbac:groups=nfd.openshift.io,resources=nodefeaturediscoveries/status,verbs=get;update
//+kubebuilder:rbac:groups=nfd.openshift.io,resources=nodefeaturediscoveries/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=create;delete;get;list;patch;watch;create
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;patch;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;watch
//+kubebuilder:rbac:groups=core,resources=nodes/finalizers,verbs=get;update;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets/status,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets/finalizers,verbs=create;get;update;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=create;get;update;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=delete;get;list;watch;create
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=delete;get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=delete;get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/eviction,verbs=delete;get;list;create
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=delete

func (r *DeviceConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	r.once.Do(func() {
		r.init(ctx)
	})
	if r.initErr != nil {
		return res, r.initErr
	}

	devConfig, err := r.helper.getRequestedDeviceConfig(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) || strings.Contains(err.Error(), "not found") {
			logger.Info("DeviceConfig CR deleted")
			r.helper.updateNodeAssignments(req.NamespacedName.String(), nil, true)
			return ctrl.Result{}, nil
		}
		return res, fmt.Errorf("failed to get the requested %s CR: %v", req.NamespacedName, err)
	}

	nodes, err := kmmmodule.GetK8SNodes(kmmmodule.MapToLabelSelector(devConfig.Spec.Selector))
	if err != nil {
		return res, fmt.Errorf("failed to list Node for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	if devConfig.GetDeletionTimestamp() != nil {
		// Reset the upgrade states
		if _, err := r.helper.handleModuleUpgrade(ctx, devConfig, nodes, true); err != nil {
			logger.Error(err, fmt.Sprintf("upgrade manager delete device config error: %v", err))
		}
		// DeviceConfig is being deleted
		err = r.helper.finalizeDeviceConfig(ctx, devConfig, nodes)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to finalize DeviceConfig %s: %v", req.NamespacedName, err)
		}
		return ctrl.Result{}, nil
	}

	// Verify that the DeviceConfig does not select nodes covered by other DeviceConfigs
	err = r.helper.validateNodeAssignments(req.NamespacedName.String(), nodes)
	if err != nil {
		if errSet := r.helper.setCondition(ctx, conditions.ConditionTypeError, devConfig, metav1.ConditionTrue, conditions.ValidationError, fmt.Sprintf("Validation failed: %v", err)); errSet != nil {
			logger.Error(fmt.Errorf("Failed to set error condition: %v", errSet), "")
		}
		if errSet := r.helper.setCondition(ctx, conditions.ConditionTypeReady, devConfig, metav1.ConditionFalse, conditions.ReadyStatus, ""); errSet != nil {
			logger.Error(fmt.Errorf("Failed to set ready condition: %v", errSet), "")
		}
		return res, err
	}

	// Validate device config
	result := r.helper.validateDeviceConfig(ctx, devConfig)
	if len(result) != 0 {
		// Update status Conditions here
		if errSet := r.helper.setCondition(ctx, conditions.ConditionTypeError, devConfig, metav1.ConditionTrue, conditions.ValidationError, fmt.Sprintf("Validation failed: %v", result)); errSet != nil {
			logger.Error(fmt.Errorf("Failed to set error condition: %v", errSet), "")
		}
		if errSet := r.helper.setCondition(ctx, conditions.ConditionTypeReady, devConfig, metav1.ConditionFalse, conditions.ReadyStatus, ""); errSet != nil {
			logger.Error(fmt.Errorf("Failed to set ready condition: %v", errSet), "")
		}
		return res, fmt.Errorf("validation failed for DeviceConfig %s: %v", req.NamespacedName, result)
	}

	err = r.helper.setFinalizer(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to set finalizer for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start build configmap reconciliation")
	err = r.helper.handleBuildConfigMap(ctx, devConfig, nodes)
	if err != nil {
		return res, fmt.Errorf("failed to handle build ConfigMap for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start module install/upgrade reconciliation")
	res, err = r.helper.handleModuleUpgrade(ctx, devConfig, nodes, false)
	if err != nil {
		return res, fmt.Errorf("Failed to fetch nodes for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start KMM reconciliation")
	if err = r.helper.handleKMMModule(ctx, devConfig, nodes); err != nil {
		return res, fmt.Errorf("failed to handle KMM module for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start device-plugin reconciliation")
	if err = r.helper.handleDevicePlugin(ctx, devConfig, nodes); err != nil {
		return res, fmt.Errorf("failed to handle device-plugin for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start kmm mod version label reconciliation")
	err = r.helper.handleKMMVersionLabel(ctx, devConfig, nodes)
	if err != nil {
		return res, fmt.Errorf("failed to handle kmm mod version label for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start node labeller reconciliation")
	err = r.helper.handleNodeLabeller(ctx, devConfig, nodes)
	if err != nil {
		return res, fmt.Errorf("failed to handle node labeller for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start metrics exporter reconciliation", "enable", devConfig.Spec.MetricsExporter.Enable)
	if err := r.helper.handleMetricsExporter(ctx, devConfig); err != nil {
		return res, fmt.Errorf("failed to handle metrics exporter for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start test runner reconciliation", "enable", devConfig.Spec.TestRunner.Enable)
	if err := r.helper.handleTestRunner(ctx, devConfig, nodes); err != nil {
		return res, fmt.Errorf("failed to handle test runner for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start config manager reconciliation", "enable", devConfig.Spec.ConfigManager.Enable)
	if err := r.helper.handleConfigManager(ctx, devConfig); err != nil {
		return res, fmt.Errorf("failed to handle config manager for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	err = r.helper.buildDeviceConfigStatus(ctx, devConfig, nodes)
	if err != nil {
		return res, fmt.Errorf("failed to build status for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	err = r.helper.updateDeviceConfigStatus(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to update status for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	// Update nodeAssignments after DeviceConfig status update
	r.helper.updateNodeAssignments(req.NamespacedName.String(), nodes, false)

	return res, nil
}

//go:generate mockgen -source=device_config_reconciler.go -package=controllers -destination=mock_device_config_reconciler.go deviceConfigReconcilerHelperAPI
type deviceConfigReconcilerHelperAPI interface {
	getRequestedDeviceConfig(ctx context.Context, namespacedName types.NamespacedName) (*amdv1alpha1.DeviceConfig, error)
	listDeviceConfigs(ctx context.Context) (*amdv1alpha1.DeviceConfigList, error)
	buildNodeAssignments(deviceConfigList *amdv1alpha1.DeviceConfigList) error
	validateNodeAssignments(namespacedName string, nodes *v1.NodeList) error
	updateNodeAssignments(namespacedName string, nodes *v1.NodeList, isFinalizer bool)
	getDeviceConfigOwnedKMMModule(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (*kmmv1beta1.Module, error)
	buildDeviceConfigStatus(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	updateDeviceConfigStatus(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	finalizeDeviceConfig(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	findDeviceConfigsForNMC(ctx context.Context, nmc client.Object) []reconcile.Request
	findDeviceConfigsForSecret(ctx context.Context, secret client.Object) []reconcile.Request
	findDeviceConfigsWithKMM(ctx context.Context, node client.Object) []reconcile.Request
	setFinalizer(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	handleKMMModule(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	handleDevicePlugin(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	handleKMMVersionLabel(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	handleBuildConfigMap(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	handleNodeLabeller(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	handleMetricsExporter(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	handleTestRunner(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	handleConfigManager(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	setCondition(ctx context.Context, condition string, devConfig *amdv1alpha1.DeviceConfig, status metav1.ConditionStatus, reason string, message string) error
	deleteCondition(ctx context.Context, condition string, devConfig *amdv1alpha1.DeviceConfig) error
	validateDeviceConfig(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) []string
	handleModuleUpgrade(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList, delete bool) (ctrl.Result, error)
}

type deviceConfigReconcilerHelper struct {
	client               client.Client
	kmmHandler           kmmmodule.KMMModuleAPI
	nlHandler            nodelabeller.NodeLabeller
	metricsHandler       metricsexporter.MetricsExporter
	testrunnerHandler    testrunner.TestRunner
	configmanagerHandler configmanager.ConfigManager
	nodeAssignments      map[string]string
	conditionUpdater     conditions.ConditionUpdater
	validator            validator.ValidatorAPI
	upgradeMgrHandler    upgradeMgrAPI
	namespace            string
}

func newDeviceConfigReconcilerHelper(client client.Client,
	kmmHandler kmmmodule.KMMModuleAPI,
	nlHandler nodelabeller.NodeLabeller,
	upgradeMgrHandler upgradeMgrAPI,
	metricsHandler metricsexporter.MetricsExporter,
	testrunnerHandler testrunner.TestRunner,
	configmanagerHandler configmanager.ConfigManager) deviceConfigReconcilerHelperAPI {
	conditionUpdater := conditions.NewDeviceConfigConditionMgr()
	validator := validator.NewValidator()
	return &deviceConfigReconcilerHelper{
		client:               client,
		kmmHandler:           kmmHandler,
		nlHandler:            nlHandler,
		metricsHandler:       metricsHandler,
		testrunnerHandler:    testrunnerHandler,
		configmanagerHandler: configmanagerHandler,
		nodeAssignments:      make(map[string]string),
		conditionUpdater:     conditionUpdater,
		validator:            validator,
		upgradeMgrHandler:    upgradeMgrHandler,
		namespace:            os.Getenv("OPERATOR_NAMESPACE"),
	}
}

func (dcrh *deviceConfigReconcilerHelper) listDeviceConfigs(ctx context.Context) (*amdv1alpha1.DeviceConfigList, error) {
	devConfigList := amdv1alpha1.DeviceConfigList{}

	if err := dcrh.client.List(ctx, &devConfigList); err != nil {
		return nil, fmt.Errorf("failed to list DeviceConfigs: %v", err)
	}

	return &devConfigList, nil
}

func (dcrh *deviceConfigReconcilerHelper) getRequestedDeviceConfig(ctx context.Context, namespacedName types.NamespacedName) (*amdv1alpha1.DeviceConfig, error) {
	devConfig := amdv1alpha1.DeviceConfig{}

	if err := dcrh.client.Get(ctx, namespacedName, &devConfig); err != nil {
		return nil, fmt.Errorf("failed to get DeviceConfig %s: %v", namespacedName, err)
	}
	return &devConfig, nil
}

// findDeviceConfigsForNMC when a NMC changed, only trigger reconcile for related DeviceConfig
func (drch *deviceConfigReconcilerHelper) findDeviceConfigsForNMC(ctx context.Context, nmc client.Object) []reconcile.Request {
	reqs := []reconcile.Request{}
	logger := log.FromContext(ctx)
	nmcObj, ok := nmc.(*kmmv1beta1.NodeModulesConfig)
	if !ok {
		logger.Error(fmt.Errorf("failed to convert object %+v to NodeModulesConfig", nmc), "")
		return reqs
	}
	if len(nmcObj.Status.Modules) > 0 {
		for _, module := range nmcObj.Status.Modules {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: module.Namespace,
					Name:      module.Name,
				},
			})
		}
	}
	return reqs
}

// findDeviceConfigsForSecret when a secret changed, only trigger reconcile for related DeviceConfig
func (drch *deviceConfigReconcilerHelper) findDeviceConfigsForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	reqs := []reconcile.Request{}
	logger := log.FromContext(ctx)
	secretObj, ok := secret.(*v1.Secret)
	if !ok {
		logger.Error(fmt.Errorf("failed to convert object %+v to Secret", secret), "")
		return reqs
	}
	if secretObj.Namespace != drch.namespace {
		return reqs
	}
	deviceConfigList, err := drch.listDeviceConfigs(ctx)
	if err != nil || deviceConfigList == nil {
		logger.Error(err, "failed to list deviceconfigs")
		return reqs
	}
	for _, dcfg := range deviceConfigList.Items {
		if dcfg.Namespace == drch.namespace &&
			drch.hasSecretReference(secretObj.Name, dcfg) {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: dcfg.Namespace,
					Name:      dcfg.Name,
				},
			})
		}
	}

	return reqs
}

func (dcrh *deviceConfigReconcilerHelper) hasSecretReference(secretName string, dcfg amdv1alpha1.DeviceConfig) bool {
	// these secrets are KMM driver build/sign/install related secrets
	// wrong configuration of them is hard to debug unless dumping logs
	// when their secrets are corrected up and a secret event kicks in
	// reconcile the corresponding deviceconfigs CRs who have references
	if dcfg.Spec.Driver.ImageRegistrySecret != nil && dcfg.Spec.Driver.ImageRegistrySecret.Name == secretName {
		return true
	}
	if dcfg.Spec.Driver.ImageSign.KeySecret != nil && dcfg.Spec.Driver.ImageSign.KeySecret.Name == secretName {
		return true
	}
	if dcfg.Spec.Driver.ImageSign.CertSecret != nil && dcfg.Spec.Driver.ImageSign.CertSecret.Name == secretName {
		return true
	}
	return false
}

// findDeviceConfigsWithKMM only reconcile deviceconfigs with KMM enabled to manage out-of-tree kernel module
func (drch *deviceConfigReconcilerHelper) findDeviceConfigsWithKMM(ctx context.Context, node client.Object) []reconcile.Request {
	reqs := []reconcile.Request{}
	logger := log.FromContext(ctx)
	deviceConfigList, err := drch.listDeviceConfigs(ctx)
	if err != nil || deviceConfigList == nil {
		logger.Error(err, "failed to list deviceconfigs")
		return reqs
	}
	for _, dcfg := range deviceConfigList.Items {
		if dcfg.Namespace == drch.namespace &&
			dcfg.Spec.Driver.Enable != nil &&
			*dcfg.Spec.Driver.Enable {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: dcfg.Namespace,
					Name:      dcfg.Name,
				},
			})
		}
	}

	return reqs
}

func (dcrh *deviceConfigReconcilerHelper) buildDeviceConfigStatus(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	// fetch DeviceConfig-owned custom resource
	// then retrieve its status and put it to DeviceConfig's status fields
	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		kmmModuleObj, err := dcrh.getDeviceConfigOwnedKMMModule(ctx, devConfig)
		if err != nil {
			return fmt.Errorf("failed to fetch owned kmm module for DeviceConfig %+v: %+v",
				types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name}, err)
		}
		if kmmModuleObj != nil {
			devConfig.Status.Drivers = amdv1alpha1.DeploymentStatus{
				NodesMatchingSelectorNumber: kmmModuleObj.Status.ModuleLoader.DesiredNumber,
				DesiredNumber:               kmmModuleObj.Status.ModuleLoader.DesiredNumber,
				AvailableNumber:             kmmModuleObj.Status.ModuleLoader.AvailableNumber,
			}
		}
	}

	devPlDs := appsv1.DaemonSet{}
	dsName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "-device-plugin",
	}

	if err := dcrh.client.Get(ctx, dsName, &devPlDs); err == nil {
		devConfig.Status.DevicePlugin = amdv1alpha1.DeploymentStatus{
			NodesMatchingSelectorNumber: devPlDs.Status.NumberAvailable,
			DesiredNumber:               devPlDs.Status.DesiredNumberScheduled,
			AvailableNumber:             devPlDs.Status.NumberAvailable,
		}
	} else {
		return fmt.Errorf("failed to fetch device-plugin %+v: %+v", dsName, err)
	}

	if devConfig.Spec.MetricsExporter.Enable != nil && *devConfig.Spec.MetricsExporter.Enable {
		metricsDS := appsv1.DaemonSet{}
		dsName := types.NamespacedName{
			Namespace: devConfig.Namespace,
			Name:      devConfig.Name + "-" + metricsexporter.ExporterName,
		}

		if err := dcrh.client.Get(ctx, dsName, &metricsDS); err == nil {
			devConfig.Status.MetricsExporter = amdv1alpha1.DeploymentStatus{
				NodesMatchingSelectorNumber: metricsDS.Status.NumberAvailable,
				DesiredNumber:               metricsDS.Status.DesiredNumberScheduled,
				AvailableNumber:             metricsDS.Status.NumberAvailable,
			}
		} else {
			return fmt.Errorf("failed to fetch metricsExporter %+v: %+v", dsName, err)
		}
	}

	// fetch latest node modules config, push their status back to DeviceConfig's status fields
	if err := dcrh.updateDeviceConfigNodeStatus(ctx, devConfig, nodes); err != nil {
		return err
	}

	// Successfully processed the config
	devConfig.Status.ObservedGeneration = devConfig.Generation
	dcrh.conditionUpdater.DeleteErrorCondition(devConfig)
	dcrh.conditionUpdater.SetReadyCondition(devConfig, metav1.ConditionTrue, conditions.ReadyStatus, "")

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) updateDeviceConfigStatus(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	// get the latest version of object right before update
	// to avoid issue "the object has been modified; please apply your changes to the latest version and try again"
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestObj, err := dcrh.getRequestedDeviceConfig(ctx, types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
		if err != nil {
			return err
		}
		devConfig.Status.DeepCopyInto(&latestObj.Status)
		if err := dcrh.client.Status().Update(ctx, latestObj); err != nil {
			return err
		}
		return nil
	})
}

func (dcrh *deviceConfigReconcilerHelper) getDeviceConfigOwnedKMMModule(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (*kmmv1beta1.Module, error) {
	module := kmmv1beta1.Module{}
	namespacedName := types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name}
	if err := dcrh.client.Get(ctx, namespacedName, &module); err != nil {
		return nil, fmt.Errorf("failed to get KMM Module %s: %v", namespacedName, err)
	}
	return &module, nil
}

func (dcrh *deviceConfigReconcilerHelper) updateDeviceConfigNodeStatus(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	logger := log.FromContext(ctx)
	previousUpgradeTimes := make(map[string]string)
	// Persist the UpgradeStartTime
	for nodeName, moduleStatus := range devConfig.Status.NodeModuleStatus {
		previousUpgradeTimes[nodeName] = moduleStatus.UpgradeStartTime
	}
	devConfig.Status.NodeModuleStatus = map[string]amdv1alpha1.ModuleStatus{}

	// for each node, fetch its status of modules configured by given DeviceConfig
	for _, node := range nodes.Items {
		// if there is no module configured for given node
		// the info under that node name will have only status and upgrade start time
		// then it will be clear to see which node didn't get module configured

		upgradeStartTime := dcrh.upgradeMgrHandler.GetNodeUpgradeStartTime(node.Name)
		//If operator restarted during Upgrade, then fetch previous known upgrade start time since the internal maps would have been cleared
		if upgradeStartTime == "" {
			upgradeStartTime = previousUpgradeTimes[node.Name]
		}
		devConfig.Status.NodeModuleStatus[node.Name] = amdv1alpha1.ModuleStatus{Status: dcrh.upgradeMgrHandler.GetNodeStatus(node.Name), UpgradeStartTime: upgradeStartTime}

		nmc := kmmv1beta1.NodeModulesConfig{}
		err := dcrh.client.Get(ctx, types.NamespacedName{Name: node.Name}, &nmc)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				logger.Error(err, fmt.Sprintf("failed to fetch NMC for node %+v", node.Name))
			}
			continue
		}
		if nmc.Status.Modules != nil {
			for _, module := range nmc.Status.Modules {
				// if there is any module was configured by given DeviceConfig
				// push their status back to DeviceConfig
				if module.Namespace == devConfig.Namespace &&
					module.Name == devConfig.Name {
					devConfig.Status.NodeModuleStatus[node.Name] = amdv1alpha1.ModuleStatus{
						ContainerImage:     module.Config.ContainerImage,
						KernelVersion:      module.Config.KernelVersion,
						LastTransitionTime: module.LastTransitionTime.String(),
						Status:             dcrh.upgradeMgrHandler.GetNodeStatus(node.Name),
						UpgradeStartTime:   upgradeStartTime,
					}
				}
			}
		}
	}

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) setFinalizer(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	if controllerutil.ContainsFinalizer(devConfig, deviceConfigFinalizer) {
		return nil
	}

	devConfigCopy := devConfig.DeepCopy()
	controllerutil.AddFinalizer(devConfig, deviceConfigFinalizer)
	return dcrh.client.Patch(ctx, devConfig, client.MergeFrom(devConfigCopy))
}

func (dcrh *deviceConfigReconcilerHelper) finalizeMetricsExporter(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	logger := log.FromContext(ctx)

	metricsSvc := v1.Service{}
	svcName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "-" + metricsexporter.ExporterName,
	}

	if err := dcrh.client.Get(ctx, svcName, &metricsSvc); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get metrics exporter service %s: %v", svcName, err)
		}
	} else {
		logger.Info("deleting metrics exporter service", "service", svcName)
		if err := dcrh.client.Delete(ctx, &metricsSvc); err != nil {
			return fmt.Errorf("failed to delete metrics exporter service %s: %v", svcName, err)
		}
	}

	metricsDS := appsv1.DaemonSet{}
	dsName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "-" + metricsexporter.ExporterName,
	}

	if err := dcrh.client.Get(ctx, dsName, &metricsDS); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get metrics exporter daemonset %s: %v", dsName, err)
		}
	} else {
		logger.Info("deleting metrics exporter daemonset", "daemonset", dsName)
		if err := dcrh.client.Delete(ctx, &metricsDS); err != nil {
			return fmt.Errorf("failed to delete metrics exporter daemonset %s: %v", dsName, err)
		}
	}

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) finalizeTestRunner(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	logger := log.FromContext(ctx)

	trDS := appsv1.DaemonSet{}
	dsName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "-" + testrunner.TestRunnerName,
	}

	if err := dcrh.client.Get(ctx, dsName, &trDS); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get test runner daemonset %s: %v", dsName, err)
		}
	} else {
		logger.Info("deleting test runner daemonset", "daemonset", dsName)
		if err := dcrh.client.Delete(ctx, &trDS); err != nil {
			return fmt.Errorf("failed to delete test runner daemonset %s: %v", dsName, err)
		}
	}

	// clean up test running node label in case test runner gets disabled during test run
	for _, node := range nodes.Items {
		// add retry logic here
		// in case Node resource is being updated by multiple clients concurrently
		if retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			updated := false
			nodeObj := &v1.Node{}
			if err := dcrh.client.Get(ctx, client.ObjectKey{Name: node.Name}, nodeObj); err != nil {
				return err
			}
			nodeObjCopy := nodeObj.DeepCopy()

			for k := range nodeObjCopy.Labels {
				if strings.HasPrefix(k, testRunnerNodeLabelPrefix) {
					delete(nodeObj.Labels, k)
					updated = true
				}
			}

			// use PATCH instead of UPDATE
			// to minimize the resource usage, compared to update the whole Node resource
			if updated {
				logger.Info(fmt.Sprintf("removing test runner labels in %v", nodeObj.Name))
				return dcrh.client.Patch(ctx, nodeObj, client.MergeFrom(nodeObjCopy))
			}

			return nil
		}); retryErr != nil {
			logger.Error(retryErr, fmt.Sprintf("failed to remove test runner labels from node %+v", node.Name))
		}
	}

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) finalizeConfigManager(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	logger := log.FromContext(ctx)

	trDS := appsv1.DaemonSet{}
	dsName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "-" + configmanager.ConfigManagerName,
	}

	if err := dcrh.client.Get(ctx, dsName, &trDS); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get config manager daemonset %s: %v", dsName, err)
		}
	} else {
		logger.Info("deleting config manager daemonset", "daemonset", dsName)
		if err := dcrh.client.Delete(ctx, &trDS); err != nil {
			return fmt.Errorf("failed to delete config manager daemonset %s: %v", dsName, err)
		}
	}

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) finalizeDeviceConfig(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	logger := log.FromContext(ctx)

	// finalize config manager before metrics exporter
	if err := dcrh.finalizeConfigManager(ctx, devConfig); err != nil {
		return err
	}

	// finalize test runner before metrics exporter
	if err := dcrh.finalizeTestRunner(ctx, devConfig, nodes); err != nil {
		return err
	}

	// finalize metrics exporter and metrics service
	// this should be removed firstly
	// because the exporter is using processes that could occupy the gpu driver
	if err := dcrh.finalizeMetricsExporter(ctx, devConfig); err != nil {
		return err
	}

	// finalize device plugin
	devPl := appsv1.DaemonSet{}
	namespacedName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "-device-plugin",
	}
	if err := dcrh.client.Get(ctx, namespacedName, &devPl); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get device-plugin daemonset %s: %v", namespacedName, err)
		}
	} else {
		logger.Info("deleting device-plugin daemonset", "daemonset", namespacedName)
		if err := dcrh.client.Delete(ctx, &devPl); err != nil {
			return fmt.Errorf("failed to delete device-plugin daemonset %s: %v", namespacedName, err)
		}
	}

	// finalize node labeller
	nlDS := appsv1.DaemonSet{}
	namespacedName = types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "-node-labeller",
	}

	if err := dcrh.client.Get(ctx, namespacedName, &nlDS); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get nodelabeller daemonset %s: %v", namespacedName, err)
		}
	} else {
		logger.Info("deleting nodelabeller daemonset", "daemonset", namespacedName)
		if err := dcrh.client.Delete(ctx, &nlDS); err != nil {
			return fmt.Errorf("failed to delete nodelabeller daemonset %s: %v", namespacedName, err)
		}
	}

	// finalize KMM CR of managing out-of-tree kernel module
	mod := kmmv1beta1.Module{}
	namespacedName = types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name,
	}
	if err := dcrh.client.Get(ctx, namespacedName, &mod); err != nil {
		if k8serrors.IsNotFound(err) {
			// if KMM module CR is not found
			if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
				logger.Info("module already deleted, removing finalizer", "module", namespacedName)
			} else {
				// driver disabled mode won't have KMM CR created
				// but it still requries the removal of node labels
				if err := dcrh.updateNodeLabels(ctx, devConfig, nodes, true); err != nil {
					logger.Error(err, "failed to update node labels")
				}
			}
			devConfigCopy := devConfig.DeepCopy()
			controllerutil.RemoveFinalizer(devConfig, deviceConfigFinalizer)
			return dcrh.client.Patch(ctx, devConfig, client.MergeFrom(devConfigCopy))
		}
		// other types of error occurred
		return fmt.Errorf("failed to get the requested Module %s: %v", namespacedName, err)
	}

	// if KMM module CR is found
	logger.Info("deleting KMM Module", "module", namespacedName)
	if err := dcrh.client.Delete(ctx, &mod); err != nil {
		return fmt.Errorf("failed to delete the requested Module: %s: %v", namespacedName, err)
	}
	if err := dcrh.updateNodeLabels(ctx, devConfig, nodes, true); err != nil {
		logger.Error(err, "failed to update node labels")
	}

	// Update nodeAssignments after DeviceConfig status update
	dcrh.updateNodeAssignments(namespacedName.String(), nodes, true)

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleBuildConfigMap(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	logger := log.FromContext(ctx)
	if devConfig.Spec.Driver.Enable == nil || !*devConfig.Spec.Driver.Enable {
		logger.Info("skip handling build config map as KMM driver mode is disabled")
		return nil
	}
	if nodes == nil || len(nodes.Items) == 0 {
		return fmt.Errorf("no nodes found for the label selector %s", kmmmodule.MapToLabelSelector(devConfig.Spec.Selector))
	}

	savedCMName := map[string]bool{}
	buildOK := true
	for _, node := range nodes.Items {
		osName, err := kmmmodule.GetOSName(node, devConfig)
		if err != nil {
			return fmt.Errorf("invalid node %s, err: %v", node.Name, err)
		}
		cmName := kmmmodule.GetCMName(osName, devConfig)
		if savedCMName[cmName] {
			// already saved a docker file for the OS-Version combo
			continue
		}

		buildDockerfileCM := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      cmName,
			},
		}

		opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, buildDockerfileCM, func() error {
			return dcrh.kmmHandler.SetBuildConfigMapAsDesired(buildDockerfileCM, devConfig)
		})

		if err == nil {
			logger.Info("Reconciled KMM build dockerfile ConfigMap", "name", buildDockerfileCM.Name, "result", opRes)
		} else {
			buildOK = false
			logger.Error(err, "error reconciling KMM build dockerfile ConfigMap", "name", buildDockerfileCM.Name, "result", opRes)
		}

		savedCMName[cmName] = true
	}

	if !buildOK {
		return errors.New("error reconciling KMM build dockerfile ConfigMap")
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleKMMModule(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	// the newly created KMM Module will always has the same namespace and name as its parent DeviceConfig
	kmmMod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: devConfig.Namespace,
			Name:      devConfig.Name,
		},
	}
	logger := log.FromContext(ctx)

	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, kmmMod, func() error {
			return dcrh.kmmHandler.SetKMMModuleAsDesired(ctx, kmmMod, devConfig, nodes)
		})

		if err == nil {
			logger.Info("Reconciled KMM Module", "name", kmmMod.Name, "result", opRes)
		}
		return err
	}
	logger.Info("skip handling KMM module as KMM driver mode is disabled")
	// if driver mode switched from enable to disable
	// we won't delete the existing KMM module

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleDevicePlugin(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	logger := log.FromContext(ctx)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-device-plugin"},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, ds, func() error {
		return dcrh.kmmHandler.SetDevicePluginAsDesired(ds, devConfig)
	})
	if err != nil {
		return err
	}
	logger.Info("Reconciled device-plugin", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleKMMVersionLabel(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	// label corresponding node with given kmod version
	// so that KMM could manage the upgrade by watching the node's version label change
	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		err := dcrh.kmmHandler.SetNodeVersionLabelAsDesired(ctx, devConfig, nodes)
		if err != nil {
			return fmt.Errorf("failed to update node version label for DeviceConfig %s/%s: %v", devConfig.Namespace, devConfig.Name, err)
		}
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleNodeLabeller(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	logger := log.FromContext(ctx)

	if devConfig.Spec.DevicePlugin.EnableNodeLabeller == nil || !*devConfig.Spec.DevicePlugin.EnableNodeLabeller {
		// deleting existing node labeller daemonset if it exists
		existingDS := &appsv1.DaemonSet{}
		existingDSMetadata := types.NamespacedName{
			Namespace: devConfig.Namespace,
			Name:      devConfig.Name + "-node-labeller",
		}
		if err := dcrh.client.Get(ctx, existingDSMetadata, existingDS); err == nil {
			logger.Info("disabling node labeller, deleting existing node labeller daemonset", "daemonset", existingDSMetadata.Name)
			if err := dcrh.client.Delete(ctx, existingDS); err != nil {
				return fmt.Errorf("failed to delete existing node labeller daemonset %s: %v", existingDSMetadata.Name, err)
			}
		}
		// clean up node labeller's label when node labeller is disabled
		// if no label need to be removed, updateNodeLabels won't send request
		if err := dcrh.updateNodeLabels(ctx, devConfig, nodes, false); err != nil {
			logger.Error(err, "failed to remove node labeller's labels when node labeller is disabled")
		}
		logger.Info("skip handling node labeller as it is disbaled", "namespace", devConfig.Namespace, "name", devConfig.Name)
		return nil
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-node-labeller"},
	}
	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, ds, func() error {
		return dcrh.nlHandler.SetNodeLabellerAsDesired(ds, devConfig)
	})

	if err != nil {
		return err
	}

	logger.Info("Reconciled node labeller", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)

	// todo: temp. cleanup labels set by node-labeller
	// not required once label cleanup is added in node-labeller
	nodeLabels := func() string {
		// nodes without gpu, kmm, dev-plugin
		sel := []string{
			"! " + utils.NodeFeatureLabelAmdGpu,
			"! " + labels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name),
			"! " + labels.GetDevicePluginNodeLabel(devConfig.Namespace, devConfig.Name),
		}

		for k, v := range devConfig.Spec.Selector {
			if k == utils.NodeFeatureLabelAmdGpu { // skip
				continue
			}
			sel = append(sel, fmt.Sprintf("%s=%s", k, v))
		}
		return strings.Join(sel, ",")
	}()

	its, err := kmmmodule.GetK8SNodes(nodeLabels)
	if err != nil {
		logger.Info("failed to get node list ", err)
		return nil
	}
	logger.Info(fmt.Sprintf("select (%v) found %v nodes", nodeLabels, len(its.Items)))

	if err := dcrh.updateNodeLabels(ctx, devConfig, its, false); err != nil {
		logger.Error(err, "failed to update node labels")
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleModuleUpgrade(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList, delete bool) (ctrl.Result, error) {
	if delete {
		return dcrh.upgradeMgrHandler.HandleDelete(ctx, devConfig, nodes)
	}
	return dcrh.upgradeMgrHandler.HandleUpgrade(ctx, devConfig, nodes)
}

func (dcrh *deviceConfigReconcilerHelper) handleMetricsExporter(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	logger := log.FromContext(ctx)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-" + metricsexporter.ExporterName},
	}

	// delete if disabled
	if devConfig.Spec.MetricsExporter.Enable == nil || !*devConfig.Spec.MetricsExporter.Enable {
		return dcrh.finalizeMetricsExporter(ctx, devConfig)
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, ds, func() error {
		return dcrh.metricsHandler.SetMetricsExporterAsDesired(ds, devConfig)
	})
	if err != nil {
		return err
	}
	logger.Info("Reconciled metrics exporter", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-" + metricsexporter.ExporterName},
	}
	opRes, err = controllerutil.CreateOrPatch(ctx, dcrh.client, svc, func() error {
		return dcrh.metricsHandler.SetMetricsServiceAsDesired(svc, devConfig)
	})

	if err != nil {
		return err
	}
	logger.Info("Reconciled metrics service", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleTestRunner(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	logger := log.FromContext(ctx)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-" + testrunner.TestRunnerName},
	}

	// delete if disabled
	// if metrics exporter is disabled, disable the test runner as well
	// because the test runner's auto unhealthy GPU watch functionality is depending on metrics exporter
	if (devConfig.Spec.TestRunner.Enable == nil || !*devConfig.Spec.TestRunner.Enable) ||
		(devConfig.Spec.MetricsExporter.Enable == nil || !*devConfig.Spec.MetricsExporter.Enable) {
		return dcrh.finalizeTestRunner(ctx, devConfig, nodes)
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, ds, func() error {
		return dcrh.testrunnerHandler.SetTestRunnerAsDesired(ds, devConfig)
	})
	if err != nil {
		return err
	}
	logger.Info("Reconciled test runner", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) handleConfigManager(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	logger := log.FromContext(ctx)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-" + configmanager.ConfigManagerName},
	}

	// delete if disabled
	if devConfig.Spec.ConfigManager.Enable == nil || !*devConfig.Spec.ConfigManager.Enable {
		return dcrh.finalizeConfigManager(ctx, devConfig)
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, ds, func() error {
		return dcrh.configmanagerHandler.SetConfigManagerAsDesired(ds, devConfig)
	})
	if err != nil {
		return err
	}
	logger.Info("Reconciled config manager", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) updateNodeLabels(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList, isFinalizer bool) error {
	logger := log.FromContext(ctx)
	labelKey, _ := kmmmodule.GetVersionLabelKV(devConfig)

	for _, node := range nodes.Items {
		// add retry logic here
		// in case Node resource is being updated by multiple clients concurrently
		if retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			updated := false
			nodeObj := &v1.Node{}
			if err := dcrh.client.Get(ctx, client.ObjectKey{Name: node.Name}, nodeObj); err != nil {
				return err
			}
			nodeObjCopy := nodeObj.DeepCopy()

			if isFinalizer {
				if _, ok := nodeObj.Labels[labelKey]; ok {
					delete(nodeObj.Labels, labelKey)
					updated = true
				}
			}

			for k := range nodeObjCopy.Labels {
				if strings.HasPrefix(k, "beta.amd.com") ||
					strings.HasPrefix(k, "amd.com") {
					delete(nodeObj.Labels, k)
					updated = true
				}
			}

			// use PATCH instead of UPDATE
			// to minimize the resource usage, compared to update the whole Node resource
			if updated {
				logger.Info(fmt.Sprintf("updating node-labeller labels in %v", nodeObj.Name))
				return dcrh.client.Patch(ctx, nodeObj, client.MergeFrom(nodeObjCopy))
			}

			return nil
		}); retryErr != nil {
			logger.Error(retryErr, fmt.Sprintf("failed to remove labels from node %+v", node.Name))
		}
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) validateNodeAssignments(namespacedName string, nodes *v1.NodeList) error {
	var err error

	for _, node := range nodes.Items {
		val, ok := dcrh.nodeAssignments[node.Name]
		if ok && val != namespacedName {
			err = fmt.Errorf("node %s already assigned to DeviceConfig %s, cannot re-assign to %s", node.Name, val, namespacedName)
			break
		}
	}

	return err
}

func (dcrh *deviceConfigReconcilerHelper) buildNodeAssignments(deviceConfigList *amdv1alpha1.DeviceConfigList) error {
	if deviceConfigList == nil {
		return nil
	}

	isReady := func(devConfig *amdv1alpha1.DeviceConfig) bool {
		ready := dcrh.conditionUpdater.GetReadyCondition(devConfig)
		if ready == nil {
			return false
		}
		return ready.Status == metav1.ConditionTrue
	}

	for _, devConfig := range deviceConfigList.Items {
		if isReady(&devConfig) {
			namespacedName := types.NamespacedName{
				Namespace: devConfig.Namespace,
				Name:      devConfig.Name,
			}

			nodeItems := []v1.Node{}
			for node := range devConfig.Status.NodeModuleStatus {
				nodeItems = append(nodeItems, v1.Node{ObjectMeta: metav1.ObjectMeta{Name: node}})
			}
			err := dcrh.validateNodeAssignments(namespacedName.String(), &v1.NodeList{Items: nodeItems})
			if err != nil {
				return err
			}
			dcrh.updateNodeAssignments(namespacedName.String(), &v1.NodeList{Items: nodeItems}, false)
		}
	}

	return nil
}

func (dcrh *deviceConfigReconcilerHelper) updateNodeAssignments(namespacedName string, nodes *v1.NodeList, isFinalizer bool) {
	if isFinalizer {
		if nodes != nil {
			for _, node := range nodes.Items {
				delete(dcrh.nodeAssignments, node.Name)
			}
		} else {
			for k, v := range dcrh.nodeAssignments {
				if v == namespacedName {
					delete(dcrh.nodeAssignments, k)
				}
			}
		}
		return
	}

	for _, node := range nodes.Items {
		dcrh.nodeAssignments[node.Name] = namespacedName
	}
}

func (dcrh *deviceConfigReconcilerHelper) setCondition(ctx context.Context, condition string, devConfig *amdv1alpha1.DeviceConfig, status metav1.ConditionStatus, reason string, message string) error {
	switch condition {
	case conditions.ConditionTypeReady:
		dcrh.conditionUpdater.SetReadyCondition(devConfig, status, reason, message)
		return dcrh.updateDeviceConfigStatus(ctx, devConfig)
	case conditions.ConditionTypeError:
		dcrh.conditionUpdater.SetErrorCondition(devConfig, status, reason, message)
		return dcrh.updateDeviceConfigStatus(ctx, devConfig)
	}
	return fmt.Errorf("Condition %s not supported", condition)
}

func (dcrh *deviceConfigReconcilerHelper) deleteCondition(ctx context.Context, condition string, devConfig *amdv1alpha1.DeviceConfig) error {
	switch condition {
	case conditions.ConditionTypeReady:
		dcrh.conditionUpdater.DeleteReadyCondition(devConfig)
		return dcrh.updateDeviceConfigStatus(ctx, devConfig)
	case conditions.ConditionTypeError:
		dcrh.conditionUpdater.DeleteErrorCondition(devConfig)
		return dcrh.updateDeviceConfigStatus(ctx, devConfig)
	}
	return fmt.Errorf("Condition %s not supported", condition)
}

func (dcrh *deviceConfigReconcilerHelper) validateDeviceConfig(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) []string {
	// Validate only if the spec has changed since the last successful validation
	if devConfig.Generation != devConfig.Status.ObservedGeneration {
		return dcrh.validator.ValidateDeviceConfigAll(ctx, dcrh.client, devConfig)
	}
	return nil
}
