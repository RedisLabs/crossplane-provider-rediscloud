// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const fixtureTaskScript = "script"
const fixtureTaskEndpoints = "endpoints"

const pscValidEndpointResponse = `{"pscServiceId":9,"endpoints":[{"id":3,"status":"active"}]}`

func TestPSCEndpointsParentIdentity(t *testing.T) { //nolint:gocyclo // Both reconcilers and each transition must be exercised.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	const name = "fixture"
	ref := xpv1.Reference{Name: exampleProviderConfig}
	for _, tc := range []struct {
		name, path string
		obj        client.Object
		run        func(client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error)
		status     func(client.Object) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, xpv1.Condition)
	}{
		{"pro", "/subscriptions/1001/private-service-connect/9", &lookupv1alpha1.PrivateServiceConnectEndpoints{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PrivateServiceConnectEndpointsSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.PrivateServiceConnectEndpointsParameters{SubscriptionID: "1001", PrivateServiceConnectServiceID: "9"}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&PrivateServiceConnectEndpointsReconciler{Client: c}).Reconcile
		}, func(o client.Object) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, xpv1.Condition) {
			v := o.(*lookupv1alpha1.PrivateServiceConnectEndpoints)
			return v.Status.AtProvider, v.Status.GetCondition(xpv1.TypeReady)
		}},
		{"active_active", "/subscriptions/1001/regions/7/private-service-connect/9", &lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.ActiveActivePrivateServiceConnectEndpointsSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ActiveActivePrivateServiceConnectEndpointsParameters{SubscriptionID: "1001", RegionID: "7", PrivateServiceConnectServiceID: "9"}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ActiveActivePrivateServiceConnectEndpointsReconciler{Client: c}).Reconcile
		}, func(o client.Object) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, xpv1.Condition) {
			v := o.(*lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints)
			return v.Status.AtProvider, v.Status.GetCondition(xpv1.TypeReady)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			paths := map[string]taskBody{
				tc.path:                                  {task: fixtureTaskEndpoints, resource: pscValidEndpointResponse},
				tc.path + "/endpoints/3/creationScripts": {task: fixtureTaskScript, resource: `{"script":{"terraformGcp":{"serviceAttachments":[]}}}`},
			}
			base := taskRoutes(t, &calls, paths)
			kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method %s", r.Method)
					http.Error(w, "unexpected", http.StatusBadRequest)
					return
				}
				base.ServeHTTP(w, r)
			}), tc.obj)
			run := tc.run(kube)
			for _, step := range []struct {
				name, resource string
				ready          corev1.ConditionStatus
				wantScript     bool
			}{
				{fixtureLookupInitial, pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"nil_endpoint", `{"pscServiceId":9,"endpoints":[{"id":3,"status":"active"},null]}`, corev1.ConditionFalse, false},
				{"after_nil", pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"duplicate_endpoint", `{"pscServiceId":9,"endpoints":[{"id":3,"status":"active"},{"id":3,"status":"active"}]}`, corev1.ConditionFalse, false},
				{"after_duplicate", pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"missing_endpoint_id", `{"pscServiceId":9,"endpoints":[{"id":3,"status":"active"},{"status":"active"}]}`, corev1.ConditionFalse, false},
				{"after_missing_id", pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"mismatch", `{"pscServiceId":10,"endpoints":[{"id":3,"status":"active"}]}`, corev1.ConditionFalse, false},
				{"missing_optional_id", `{"endpoints":[{"id":3,"status":"active"}]}`, corev1.ConditionTrue, true},
				{fixtureLookupRecovery, pscValidEndpointResponse, corev1.ConditionTrue, true},
			} {
				t.Run(step.name, func(t *testing.T) {
					paths[tc.path] = taskBody{task: fixtureTaskEndpoints, resource: step.resource}
					before := len(calls)
					result, err := run(context.Background(), request(name))
					if err != nil {
						t.Fatal(err)
					}
					delay := requeue
					if !step.wantScript {
						delay = requeueFailed
					}
					if result.RequeueAfter != delay {
						t.Fatalf("unexpected requeue %s", result.RequeueAfter)
					}
					newCalls := strings.Join(calls[before:], "\n")
					hasScript := strings.Contains(newCalls, "/creationScripts")
					if hasScript != step.wantScript {
						t.Fatalf("unexpected script fetch: %s", newCalls)
					}
					if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, tc.obj); err != nil {
						t.Fatal(err)
					}
					observation, ready := tc.status(tc.obj)
					if ready.Status != step.ready {
						t.Fatalf("unexpected condition: %+v", ready)
					}
					if !step.wantScript {
						if !reflect.DeepEqual(observation, lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}) {
							t.Fatal("stale observation not cleared")
						}
						if ready.Message != fixtureLookupSafeError {
							t.Fatalf("unsafe condition: %q", ready.Message)
						}
					} else if observation.ID != "1001/9" || len(observation.Endpoints) != 1 {
						t.Fatalf("unexpected observation: %+v", observation)
					}
				})
			}
		})
	}
}

func TestPSCEndpointsMalformedTaskRecovery(t *testing.T) { //nolint:gocyclo // Both reconcilers and each transition must be exercised.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	const name = "fixture"
	ref := xpv1.Reference{Name: exampleProviderConfig}
	for _, tc := range []struct {
		name, path string
		obj        client.Object
		run        func(client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error)
		status     func(client.Object) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, xpv1.Condition)
	}{
		{"pro", "/subscriptions/1001/private-service-connect/9", &lookupv1alpha1.PrivateServiceConnectEndpoints{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PrivateServiceConnectEndpointsSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.PrivateServiceConnectEndpointsParameters{SubscriptionID: "1001", PrivateServiceConnectServiceID: "9"}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&PrivateServiceConnectEndpointsReconciler{Client: c}).Reconcile
		}, func(o client.Object) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, xpv1.Condition) {
			v := o.(*lookupv1alpha1.PrivateServiceConnectEndpoints)
			return v.Status.AtProvider, v.Status.GetCondition(xpv1.TypeReady)
		}},
		{"active_active", "/subscriptions/1001/regions/7/private-service-connect/9", &lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.ActiveActivePrivateServiceConnectEndpointsSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ActiveActivePrivateServiceConnectEndpointsParameters{SubscriptionID: "1001", RegionID: "7", PrivateServiceConnectServiceID: "9"}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ActiveActivePrivateServiceConnectEndpointsReconciler{Client: c}).Reconcile
		}, func(o client.Object) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, xpv1.Condition) {
			v := o.(*lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints)
			return v.Status.AtProvider, v.Status.GetCondition(xpv1.TypeReady)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			var phase atomic.Value
			phase.Store("")
			paths := map[string]taskBody{
				tc.path:                                  {task: fixtureTaskEndpoints, resource: pscValidEndpointResponse},
				tc.path + "/endpoints/3/creationScripts": {task: fixtureTaskScript, resource: `{"script":{"terraformGcp":{"serviceAttachments":[]}}}`},
			}
			base := taskRoutes(t, &calls, paths)
			kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
					t.Errorf("unexpected method %s", r.Method)
					http.Error(w, "unexpected", http.StatusBadRequest)
					return
				}
				target := ""
				currentPhase := phase.Load().(string)
				switch currentPhase {
				case "list_missing_task", "list_http_error":
					target = tc.path
				case "list_missing_resource":
					target = "/tasks/endpoints"
				case "script_missing_task":
					target = tc.path + "/endpoints/3/creationScripts"
				case "script_missing_resource":
					target = "/tasks/script"
				}
				if target != "" && r.URL.Path == target {
					calls = append(calls, r.Method+" "+r.URL.RequestURI())
					if currentPhase == "list_http_error" {
						http.Error(w, "sentinel-secret", http.StatusForbidden)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if currentPhase == "list_missing_resource" || currentPhase == "script_missing_resource" {
						_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"sensitive":"sentinel-secret"}}`)
					} else {
						_, _ = io.WriteString(w, `{"sensitive":"sentinel-secret"}`)
					}
					return
				}
				base.ServeHTTP(w, r)
			}), tc.obj)
			run := tc.run(kube)
			for _, step := range []struct {
				name, resource string
				ready          corev1.ConditionStatus
				wantScript     bool
			}{
				{fixtureLookupInitial, pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"list_missing_task", pscValidEndpointResponse, corev1.ConditionFalse, false},
				{"list_recovery_one", pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"list_missing_resource", pscValidEndpointResponse, corev1.ConditionFalse, false},
				{"list_recovery_two", pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"script_missing_task", pscValidEndpointResponse, corev1.ConditionFalse, true},
				{"script_recovery_one", pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"script_missing_resource", pscValidEndpointResponse, corev1.ConditionFalse, true},
				{"script_recovery_two", pscValidEndpointResponse, corev1.ConditionTrue, true},
				{"list_http_error", pscValidEndpointResponse, corev1.ConditionFalse, false},
				{"http_recovery", pscValidEndpointResponse, corev1.ConditionTrue, true},
			} {
				t.Run(step.name, func(t *testing.T) {
					phase.Store(step.name)
					paths[tc.path] = taskBody{task: fixtureTaskEndpoints, resource: step.resource}
					before := len(calls)
					result, err := run(context.Background(), request(name))
					if err != nil {
						t.Fatal(err)
					}
					delay := requeue
					if step.ready == corev1.ConditionFalse {
						delay = requeueFailed
					}
					if result.RequeueAfter != delay {
						t.Fatalf("unexpected requeue %s", result.RequeueAfter)
					}
					newCalls := strings.Join(calls[before:], "\n")
					hasScript := strings.Contains(newCalls, "/creationScripts")
					if hasScript != step.wantScript {
						t.Fatalf("unexpected script fetch: %s", newCalls)
					}
					if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, tc.obj); err != nil {
						t.Fatal(err)
					}
					observation, ready := tc.status(tc.obj)
					if ready.Status != step.ready {
						t.Fatalf("unexpected condition: %+v", ready)
					}
					if step.ready == corev1.ConditionFalse {
						if !reflect.DeepEqual(observation, lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}) {
							t.Fatal("stale observation not cleared")
						}
						if ready.Message != fixtureLookupSafeError {
							t.Fatalf("unsafe condition: %q", ready.Message)
						}
					} else if observation.ID != "1001/9" || len(observation.Endpoints) != 1 {
						t.Fatalf("unexpected observation: %+v", observation)
					}
				})
			}
		})
	}
}
