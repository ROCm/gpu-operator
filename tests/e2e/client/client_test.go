/*
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

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

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func TestDeleteDeviceConfigUsesNamedRequest(t *testing.T) {
	if err := amdv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodDelete || r.URL.Path != "/apis/amd.com/v1alpha1/namespaces/test/deviceconfigs/fixture" || r.URL.RawQuery != "" {
			t.Errorf("unexpected delete request: %s %s", r.Method, r.URL)
			http.Error(w, "refuse collection deletion", http.StatusBadRequest)
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["kind"] != "DeleteOptions" || body["apiVersion"] != amdv1alpha1.GroupVersion.String() {
			t.Errorf("expected DeleteOptions, got %v", body)
		}
		if _, ok := body["metadata"]; ok {
			t.Errorf("object metadata is not a deletion selector: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Success"}`))
	}))
	defer server.Close()
	cli, err := Client(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.DeviceConfigs("test").Delete("fixture"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one named request, got %d", calls)
	}
	if _, err := cli.DeviceConfigs("test").Delete(""); err == nil {
		t.Fatal("empty name must not issue a collection DELETE")
	}
	if calls != 1 {
		t.Fatal("empty name reached the server")
	}
}
