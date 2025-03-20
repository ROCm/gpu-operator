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

package conditions

import (
	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition Type
const (
	ConditionTypeReady = "Ready"
	ConditionTypeError = "Error"
)

// Condition Reason
const (
	// ValidationError is the reason for all validation errors
	ValidationError = "ValidationError"
	// ErrorStatus is the generic error state
	ErrorStatus = "Error"
	// ReadyStatus represents operator in ready and healthy state
	ReadyStatus = "OperatorReady"
)

type ConditionManager struct{}

func NewDeviceConfigConditionMgr() ConditionUpdater {
	return &ConditionManager{}
}

func (cm *ConditionManager) SetReadyCondition(cr any, status metav1.ConditionStatus, reason, message string) {
	devConfig := cr.(*amdv1alpha1.DeviceConfig)
	setCondition(devConfig, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})
}

func (cm *ConditionManager) GetReadyCondition(cr any) *metav1.Condition {
	devConfig := cr.(*amdv1alpha1.DeviceConfig)
	return findCondition(devConfig.Status.Conditions, ConditionTypeReady)
}

func (cm *ConditionManager) SetErrorCondition(cr any, status metav1.ConditionStatus, reason string, message string) {
	devConfig := cr.(*amdv1alpha1.DeviceConfig)
	setCondition(devConfig, metav1.Condition{
		Type:               ConditionTypeError,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})
}

func (cm *ConditionManager) DeleteReadyCondition(cr any) {
	devConfig := cr.(*amdv1alpha1.DeviceConfig)
	deleteCondition(&devConfig.Status.Conditions, ConditionTypeReady)
}

func (cm *ConditionManager) DeleteErrorCondition(cr any) {
	devConfig := cr.(*amdv1alpha1.DeviceConfig)
	deleteCondition(&devConfig.Status.Conditions, ConditionTypeError)
}

func setCondition(devConfig *amdv1alpha1.DeviceConfig, newCondition metav1.Condition) {
	existingCondition := findCondition(devConfig.Status.Conditions, newCondition.Type)

	if existingCondition != nil {
		if existingCondition.Status == newCondition.Status {
			newCondition.LastTransitionTime = existingCondition.LastTransitionTime
		}
		*existingCondition = newCondition
	} else {
		devConfig.Status.Conditions = append(devConfig.Status.Conditions, newCondition)
	}
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

func deleteCondition(conditions *[]metav1.Condition, conditionType string) {
	var updatedConditions []metav1.Condition
	for _, condition := range *conditions {
		if condition.Type != conditionType {
			updatedConditions = append(updatedConditions, condition)
		}
	}
	*conditions = updatedConditions
}
