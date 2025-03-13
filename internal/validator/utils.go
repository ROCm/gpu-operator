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

package validator

import (
	"context"
	"fmt"

	"k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func validateSecret(ctx context.Context, client client.Client, secretRef *v1.LocalObjectReference, namespace string) error {
	if secretRef == nil || secretRef.Name == "" {
		return fmt.Errorf("Secret reference is nil or empty")
	}

	secret := &v1.Secret{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretRef.Name}, secret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("Secret %s not found in namespace %s", secretRef.Name, namespace)
		}
		return fmt.Errorf("failed to get Secret %s: %v", secretRef.Name, err)
	}

	return nil
}

func validateConfigMap(ctx context.Context, client client.Client, mapRef string, namespace string) error {
	if mapRef == "" {
		return fmt.Errorf("No ConfigMap name provided for validation")
	}

	configMap := &v1.ConfigMap{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: mapRef}, configMap)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("ConfigMap %s not found in namespace %s", mapRef, namespace)
		}
		return fmt.Errorf("failed to get ConfigMap %s: %v", mapRef, err)
	}

	return nil
}
