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
	"fmt"

	"github.com/ROCm/gpu-operator/internal/configmanager"
	"github.com/ROCm/gpu-operator/internal/metricsexporter"
	"github.com/ROCm/gpu-operator/internal/testrunner"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/internal/nodelabeller"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	devConfigName      = "devConfigName"
	devConfigNamespace = "devConfigNamespace"
)

var (
	testNodeList = &v1.NodeList{
		Items: []v1.Node{
			{
				TypeMeta: metav1.TypeMeta{
					Kind: "Node",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "unit-test-node",
				},
				Spec: v1.NodeSpec{},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{
						Architecture:            "amd64",
						ContainerRuntimeVersion: "containerd://1.7.19",
						KernelVersion:           "6.8.0-40-generic",
						KubeProxyVersion:        "v1.30.3",
						KubeletVersion:          "v1.30.3",
						OperatingSystem:         "linux",
						OSImage:                 "Ubuntu 22.04.3 LTS",
					},
				},
			},
		},
	}
)

// FIX ME
//var _ = Describe("Reconcile", func() {
//	var (
//		mockHelper *MockdeviceConfigReconcilerHelperAPI
//		dcr        *DeviceConfigReconciler
//	)
//
//	BeforeEach(func() {
//		ctrl := gomock.NewController(GinkgoT())
//		mockHelper = NewMockdeviceConfigReconcilerHelperAPI(ctrl)
//		dcr = &DeviceConfigReconciler{
//			helper: mockHelper,
//		}
//	})
//
//	ctx := context.Background()
//	nn := types.NamespacedName{
//		Name:      devConfigName,
//		Namespace: devConfigNamespace,
//	}
//	req := ctrl.Request{NamespacedName: nn}
//
//	DescribeTable("reconciler error flow", func(getDeviceError,
//		setFinalizerError,
//		buildConfigMapError,
//		handleKMMModuleError,
//		handleNodeLabellerError,
//		handleMetricsError bool) {
//		devConfig := &amdv1alpha1.DeviceConfig{}
//		if getDeviceError {
//			mockHelper.EXPECT().getRequestedDeviceConfig(ctx, nn).Return(nil, fmt.Errorf("some error"))
//			goto executeTestFunction
//		}
//		mockHelper.EXPECT().getRequestedDeviceConfig(ctx, req.NamespacedName).Return(devConfig, nil)
//		if setFinalizerError {
//			mockHelper.EXPECT().setFinalizer(ctx, devConfig).Return(fmt.Errorf("some error"))
//			goto executeTestFunction
//		}
//		mockHelper.EXPECT().setFinalizer(ctx, devConfig).Return(nil)
//		if buildConfigMapError {
//			mockHelper.EXPECT().handleBuildConfigMap(ctx, devConfig, testNodeList).Return(fmt.Errorf("some error"))
//			goto executeTestFunction
//		}
//		mockHelper.EXPECT().handleBuildConfigMap(ctx, devConfig, testNodeList).Return(nil)
//		if handleKMMModuleError {
//			mockHelper.EXPECT().handleKMMModule(ctx, devConfig, testNodeList).Return(fmt.Errorf("some error"))
//			goto executeTestFunction
//		}
//		mockHelper.EXPECT().handleKMMModule(ctx, devConfig, testNodeList).Return(nil)
//		if handleNodeLabellerError {
//			mockHelper.EXPECT().handleNodeLabeller(ctx, devConfig).Return(fmt.Errorf("some error"))
//			goto executeTestFunction
//		}
//		mockHelper.EXPECT().handleNodeLabeller(ctx, devConfig).Return(nil)
//
//	executeTestFunction:
//
//		res, err := dcr.Reconcile(ctx, req)
//		if getDeviceError || setFinalizerError || buildConfigMapError || handleKMMModuleError || handleNodeLabellerError || handleMetricsError {
//			Expect(err).To(HaveOccurred())
//		} else {
//			Expect(err).ToNot(HaveOccurred())
//			Expect(res).To(Equal(ctrl.Result{}))
//		}
//	},
//		Entry("good flow, no requeue", false, false, false, false, false, false),
//		Entry("getDeviceConfigFailed", true, false, false, false, false, false),
//		Entry("setFinalizer failed", false, true, false, false, false, false),
//		Entry("buildConfigMap failed", false, false, true, false, false, false),
//		Entry("handleKMMModule failed", false, false, false, true, false, false),
//		Entry("handleNodeLabeller failed", false, false, false, false, true, false),
//		Entry("handleMetrics failed", false, false, false, false, false, true),
//	)
//
//	It("device config finalization", func() {
//		devConfig := &amdv1alpha1.DeviceConfig{}
//		devConfig.SetDeletionTimestamp(&metav1.Time{})
//
//		mockHelper.EXPECT().getRequestedDeviceConfig(ctx, req.NamespacedName).Return(devConfig, nil)
//		mockHelper.EXPECT().finalizeDeviceConfig(ctx, devConfig, testNodeList).Return(nil)
//
//		res, err := dcr.Reconcile(ctx, req)
//
//		Expect(err).ToNot(HaveOccurred())
//		Expect(res).To(Equal(ctrl.Result{}))
//
//		mockHelper.EXPECT().getRequestedDeviceConfig(ctx, req.NamespacedName).Return(devConfig, nil)
//		mockHelper.EXPECT().finalizeDeviceConfig(ctx, devConfig, testNodeList).Return(fmt.Errorf("some error"))
//
//		res, err = dcr.Reconcile(ctx, req)
//		Expect(err).To(HaveOccurred())
//		Expect(res).To(Equal(ctrl.Result{}))
//	})
//})

var _ = Describe("getLabelsPerModules", func() {
	var (
		kubeClient *mock_client.MockClient
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nil, nil, nil, nil, nil)
	})

	ctx := context.Background()
	nn := types.NamespacedName{
		Name:      devConfigName,
		Namespace: devConfigNamespace,
	}

	It("good flow", func() {
		expectedDevConfig := amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
		}
		kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Do(
			func(_ interface{}, _ interface{}, devConfig *amdv1alpha1.DeviceConfig, _ ...client.GetOption) {
				devConfig.Name = nn.Name
				devConfig.Namespace = nn.Namespace
			},
		)
		res, err := dcrh.getRequestedDeviceConfig(ctx, nn)
		Expect(err).ToNot(HaveOccurred())
		Expect(*res).To(Equal(expectedDevConfig))
	})

	It("error flow", func() {
		kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(fmt.Errorf("some error"))

		res, err := dcrh.getRequestedDeviceConfig(ctx, nn)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})
})

var _ = Describe("setFinalizer", func() {
	var (
		kubeClient *mock_client.MockClient
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nil, nil, nil, nil, nil)
	})

	ctx := context.Background()

	It("good flow", func() {
		devConfig := &amdv1alpha1.DeviceConfig{}

		kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil)

		err := dcrh.setFinalizer(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())

		err = dcrh.setFinalizer(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})

	It("error flow", func() {
		devConfig := &amdv1alpha1.DeviceConfig{}

		kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		err := dcrh.setFinalizer(ctx, devConfig)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("finalizeDeviceConfig", func() {
	var (
		kubeClient *mock_client.MockClient
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nil, nil, nil, nil, nil)
	})

	ctx := context.Background()
	driverEnable := true
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{
				Enable: &driverEnable,
			},
		},
	}

	nodeLabellerNN := types.NamespacedName{
		Name:      devConfigName + "-node-labeller",
		Namespace: devConfigNamespace,
	}

	devPluginNN := types.NamespacedName{
		Name:      devConfigName + "-device-plugin",
		Namespace: devConfigNamespace,
	}

	metricsNN := types.NamespacedName{
		Name:      devConfigName + "-" + metricsexporter.ExporterName,
		Namespace: devConfigNamespace,
	}

	testrunnerNN := types.NamespacedName{
		Name:      devConfigName + "-" + testrunner.TestRunnerName,
		Namespace: devConfigNamespace,
	}

	configmanagerNN := types.NamespacedName{
		Name:      devConfigName + "-" + configmanager.ConfigManagerName,
		Namespace: devConfigNamespace,
	}

	nn := types.NamespacedName{
		Name:      devConfigName,
		Namespace: devConfigNamespace,
	}

	testNodeNN := types.NamespacedName{
		Name: "unit-test-node",
	}

	It("failed to get NodeLabeller daemonset", func() {
		statusErr := &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonNotFound,
			},
		}

		kubeClient.EXPECT().Get(ctx, devPluginNN, gomock.Any()).Return(statusErr).Times(1)
		kubeClient.EXPECT().Get(ctx, configmanagerNN, gomock.Any()).Return(statusErr).Times(1)
		kubeClient.EXPECT().Get(ctx, testrunnerNN, gomock.Any()).Return(statusErr).Times(1)
		kubeClient.EXPECT().Get(ctx, testNodeNN, gomock.Any()).Return(nil).Times(1)
		kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(statusErr).Times(2)
		kubeClient.EXPECT().Get(ctx, nodeLabellerNN, gomock.Any()).Return(fmt.Errorf("some error"))

		err := dcrh.finalizeDeviceConfig(ctx, devConfig, testNodeList)
		Expect(err).To(HaveOccurred())
	})

	// FIX ME
	//It("node labeller daemonset exists", func() {
	//	gomock.InOrder(
	//		kubeClient.EXPECT().Get(ctx, nodeLabellerNN, gomock.Any()).Return(nil),
	//		kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(nil),
	//		kubeClient.EXPECT().Delete(ctx, gomock.Any()).Return(nil),
	//	)
	//
	//	err := dcrh.finalizeDeviceConfig(ctx, devConfig, testNodeList)
	//	Expect(err).To(BeNil())
	//})
	//
	//It("failed to get labeller daemonset", func() {
	//	gomock.InOrder(
	//		kubeClient.EXPECT().Get(ctx, nodeLabellerNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
	//	)
	//
	//	err := dcrh.finalizeDeviceConfig(ctx, devConfig, testNodeList)
	//	Expect(err).To(HaveOccurred())
	//})

	It("node metrics daemonset exists", func() {
		statusErr := &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonNotFound,
			},
		}

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, configmanagerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testrunnerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testNodeNN, gomock.Any()).Return(nil).Times(1),
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(statusErr).Times(2),
			kubeClient.EXPECT().Get(ctx, devPluginNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, nodeLabellerNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
			kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(nil),
			kubeClient.EXPECT().Delete(ctx, gomock.Any()).Return(nil),
			kubeClient.EXPECT().Get(ctx, testNodeNN, gomock.Any()).Return(nil),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig, testNodeList)
		Expect(err).To(BeNil())
	})

	It("failed to get KMM Module", func() {
		statusErr := &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonNotFound,
			},
		}

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, configmanagerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testrunnerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testNodeNN, gomock.Any()).Return(nil).Times(1),
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(statusErr).Times(2),
			kubeClient.EXPECT().Get(ctx, devPluginNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, nodeLabellerNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
			kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(fmt.Errorf("some error")),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig, testNodeList)
		Expect(err).To(HaveOccurred())
	})

	It("KMM module not found, removing finalizer", func() {
		statusErr := &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonNotFound,
			},
		}

		expectedDevConfig := devConfig.DeepCopy()
		expectedDevConfig.SetFinalizers([]string{})
		controllerutil.AddFinalizer(devConfig, deviceConfigFinalizer)

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, configmanagerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testrunnerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testNodeNN, gomock.Any()).Return(nil).Times(1),
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(statusErr).Times(2),
			kubeClient.EXPECT().Get(ctx, devPluginNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, nodeLabellerNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
			kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "moduleName")),
			kubeClient.EXPECT().Patch(ctx, expectedDevConfig, gomock.Any()).Return(nil),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})

	It("KMM module found, deleting it", func() {
		statusErr := &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonNotFound,
			},
		}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      devConfigName,
				Namespace: devConfigNamespace,
			},
		}

		expectedDevConfig := devConfig.DeepCopy()
		expectedDevConfig.SetFinalizers([]string{})
		controllerutil.AddFinalizer(devConfig, deviceConfigFinalizer)

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, configmanagerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testrunnerNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, testNodeNN, gomock.Any()).Return(nil).Times(1),
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(statusErr).Times(2),
			kubeClient.EXPECT().Get(ctx, devPluginNN, gomock.Any()).Return(statusErr).Times(1),
			kubeClient.EXPECT().Get(ctx, nodeLabellerNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
			kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Do(
				func(_ interface{}, _ interface{}, mod *kmmv1beta1.Module, _ ...client.GetOption) {
					mod.Name = nn.Name
					mod.Namespace = nn.Namespace
				},
			),
			kubeClient.EXPECT().Delete(ctx, &mod).Return(nil),
			kubeClient.EXPECT().Get(ctx, testNodeNN, gomock.Any()).Return(nil),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("handleKMMModule", func() {
	var (
		kubeClient *mock_client.MockClient
		kmmHelper  *kmmmodule.MockKMMModuleAPI
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		kmmHelper = kmmmodule.NewMockKMMModuleAPI(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, kmmHelper, nil, nil, nil, nil, nil)
	})

	ctx := context.Background()
	driverEnable := true
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{
				Enable: &driverEnable,
			},
		},
	}

	It("KMM Module does not exist", func() {
		newMod := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      devConfig.Name,
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "whatever")),
			kmmHelper.EXPECT().SetKMMModuleAsDesired(ctx, newMod, devConfig, testNodeList).Return(nil),

			kubeClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dcrh.handleKMMModule(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})

	It("KMM Module exists", func() {
		existingMod := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      devConfig.Name,
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
				func(_ interface{}, _ interface{}, mod *kmmv1beta1.Module, _ ...client.GetOption) {
					mod.Name = devConfig.Name
					mod.Namespace = devConfig.Namespace
				},
			),
			kmmHelper.EXPECT().SetKMMModuleAsDesired(ctx, existingMod, devConfig, testNodeList).Return(nil),
		)

		err := dcrh.handleKMMModule(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("handleBuildConfigMap", func() {
	var (
		kubeClient *mock_client.MockClient
		kmmHelper  *kmmmodule.MockKMMModuleAPI
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		kmmHelper = kmmmodule.NewMockKMMModuleAPI(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, kmmHelper, nil, nil, nil, nil, nil)
	})

	ctx := context.Background()
	driverEnable := true
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{
				Enable: &driverEnable,
			},
		},
	}

	It("BuildConfig does not exist", func() {
		newBuildCM := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      kmmmodule.GetCMName("ubuntu-22.04", devConfig),
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "whatever")),
			kmmHelper.EXPECT().SetBuildConfigMapAsDesired(newBuildCM, devConfig).Return(nil),
			kubeClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dcrh.handleBuildConfigMap(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})

	It("BuildConfig exists", func() {
		existingBuildCM := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      kmmmodule.GetCMName("ubuntu-22.04", devConfig),
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
				func(_ interface{}, _ interface{}, buildCM *v1.ConfigMap, _ ...client.GetOption) {
					buildCM.Name = kmmmodule.GetCMName("ubuntu-22.04", devConfig)
					buildCM.Namespace = devConfig.Namespace
				},
			),
			kmmHelper.EXPECT().SetBuildConfigMapAsDesired(existingBuildCM, devConfig).Return(nil),
		)

		err := dcrh.handleBuildConfigMap(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("handleNodeLabeller", func() {
	var (
		kubeClient         *mock_client.MockClient
		nodeLabellerHelper *nodelabeller.MockNodeLabeller
		dcrh               deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		nodeLabellerHelper = nodelabeller.NewMockNodeLabeller(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nodeLabellerHelper, nil, nil, nil, nil)
	})

	ctx := context.Background()
	enableNodeLabeller := true
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
		Spec: amdv1alpha1.DeviceConfigSpec{
			DevicePlugin: amdv1alpha1.DevicePluginSpec{
				EnableNodeLabeller: &enableNodeLabeller,
			},
		},
	}

	It("NodeLabeller DaemonSet does not exist", func() {
		newDS := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-node-labeller"},
		}

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "whatever")),
			nodeLabellerHelper.EXPECT().SetNodeLabellerAsDesired(newDS, devConfig).Return(nil),
			kubeClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dcrh.handleNodeLabeller(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})

	It("NodeLabeller DaemonSet exists", func() {
		existingDS := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-node-labeller"},
		}

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
				func(_ interface{}, _ interface{}, ds *appsv1.DaemonSet, _ ...client.GetOption) {
					ds.Name = devConfig.Name + "-node-labeller"
					ds.Namespace = devConfig.Namespace
				},
			),
			nodeLabellerHelper.EXPECT().SetNodeLabellerAsDesired(existingDS, devConfig).Return(nil),
		)

		err := dcrh.handleNodeLabeller(ctx, devConfig, testNodeList)
		Expect(err).ToNot(HaveOccurred())
	})
})
