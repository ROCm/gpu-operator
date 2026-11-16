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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

func TestDeleteDeviceConfigUsesNamedRequest(t *testing.T) {
	// Use the same serializer setup as the e2e client, without registering
	// DeviceConfig in the Kubernetes scheme just for this test.
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
		if body["kind"] != "DeleteOptions" || body["apiVersion"] != "v1" {
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

func TestDeleteDeviceConfigResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		body       string
		checkError func(error) bool
	}{
		{
			name:       "immediate deletion returns Status",
			statusCode: http.StatusOK,
			body:       `{"apiVersion":"v1","kind":"Status","status":"Success"}`,
		},
		{
			name:       "finalizer pending returns DeviceConfig",
			statusCode: http.StatusAccepted,
			body:       `{"apiVersion":"amd.com/v1alpha1","kind":"DeviceConfig","metadata":{"name":"fixture","namespace":"test","deletionTimestamp":"2026-11-16T00:00:00Z","finalizers":["amd.com/deviceconfig-finalizer"]},"status":{}}`,
		},
		{
			name:       "not found remains an error",
			statusCode: http.StatusNotFound,
			body:       `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","code":404,"message":"deviceconfigs.amd.com fixture not found"}`,
			checkError: apierrors.IsNotFound,
		},
		{
			name:       "forbidden remains an error",
			statusCode: http.StatusForbidden,
			body:       `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"deviceconfigs.amd.com fixture is forbidden"}`,
			checkError: apierrors.IsForbidden,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			cli, err := Client(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = cli.DeviceConfigs("test").Delete("fixture")
			if tc.checkError != nil {
				if !tc.checkError(err) {
					t.Fatalf("unexpected delete error: %v", err)
				}
			} else if err != nil {
				t.Fatalf("successful delete returned an error: %v", err)
			}
		})
	}
}
