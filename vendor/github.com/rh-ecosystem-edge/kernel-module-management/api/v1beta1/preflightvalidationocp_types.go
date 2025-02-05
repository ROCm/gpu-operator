/*


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

package v1beta1

import (
	"fmt"

	"github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// PreflightValidationOCPSpec describes the desired state of the resource, such as the OCP release image
// that Module CRs need to be verified against as well as the push image flag
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +kubebuilder:validation:Required
// +kubebuilder:object:generate=false
type PreflightValidationOCPSpec = v1beta2.PreflightValidationOCPSpec

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PreflightValidationOCP initiates a preflight validations for all Modules on the current OCP cluster.
// +kubebuilder:resource:path=preflightvalidationsocp,scope=Cluster
// +kubebuilder:resource:path=preflightvalidationsocp,scope=Cluster,shortName=pfvo
// +operator-sdk:csv:customresourcedefinitions:displayName="Preflight Validation OCP"
type PreflightValidationOCP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:validation:Required

	Spec   v1beta2.PreflightValidationOCPSpec `json:"spec,omitempty"`
	Status PreflightValidationStatus          `json:"status,omitempty"`
}

func (p *PreflightValidationOCP) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.PreflightValidationOCP)

	dst.ObjectMeta = p.ObjectMeta
	dst.Spec = p.Spec

	var err error

	dst.Status, err = v1beta2StatusFromV1beta1(p.Status)
	if err != nil {
		return fmt.Errorf("error while converting status: %v", err)
	}

	return nil
}

func (p *PreflightValidationOCP) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.PreflightValidationOCP)

	p.ObjectMeta = src.ObjectMeta
	p.Spec = src.Spec
	p.Status = v1beta1StatusFromV1beta2(src.Status)

	return nil
}

// +kubebuilder:object:root=true

// PreflightValidationList is a list of PreflightValidation objects.
type PreflightValidationOCPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of PreflightValidation. More info:
	// https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md
	Items []PreflightValidationOCP `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PreflightValidationOCP{}, &PreflightValidationOCPList{})
}
