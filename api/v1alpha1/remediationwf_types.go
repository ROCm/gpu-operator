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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Namespaced,shortName=rwfstatus
//+kubebuilder:subresource:status

// RemediationWorkflowStatus keeps a record of recent remediation workflow runs.
// We maintain this information to avoid re-running remediation workflows on nodes where a pre-defined threshold is crossed.
// +operator-sdk:csv:customresourcedefinitions:displayName="RemediationWorkflowStatus",resources={{Module,v1beta1,modules.kmm.sigs.x-k8s.io},{Daemonset,v1,apps},{services,v1,core},{Pod,v1,core}}
type RemediationWorkflowStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Status field holds remediation workflow run history for each node and node condition
	// Key is node name. Value is a map with key as node condition and value as list of workflow metadata(workflow name and it's start time)
	Status map[string]map[string][]WorkflowMetadata `json:"status,omitempty"`
}

type WorkflowMetadata struct {
	Name      string `json:"name,omitempty"`
	StartTime string `json:"startTime,omitempty"`
}

//+kubebuilder:object:root=true

// RemediationWorkflowStatusList contains a list of RemediationWorkflowStatuses
type RemediationWorkflowStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []RemediationWorkflowStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemediationWorkflowStatus{}, &RemediationWorkflowStatusList{})
}
