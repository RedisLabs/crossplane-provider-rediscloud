// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const identityLinkTask = "identity-link"

func TestPrivateLinkIdentityReconcile(t *testing.T) { //nolint:gocyclo // Both resource paths and each transition are checked.
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("hosted Actions only")
	}
	const name = "fixture"
	ref := xpv1.Reference{Name: exampleProviderConfig}
	for _, tc := range []struct {
		name, path string
		obj        client.Object
		run        func(client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error)
		status     func(client.Object) (lookupv1alpha1.PrivateLinkObservation, xpv1.Condition)
		steps      []identityStep
	}{
		{"pro", "/subscriptions/1001/private-link", &lookupv1alpha1.PrivateLink{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PrivateLinkSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "1001"}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&PrivateLinkReconciler{Client: c}).Reconcile
		}, func(o client.Object) (lookupv1alpha1.PrivateLinkObservation, xpv1.Condition) {
			v := o.(*lookupv1alpha1.PrivateLink)
			return v.Status.AtProvider, v.Status.GetCondition(xpv1.TypeReady)
		}, []identityStep{{"initial", `{"subscriptionId":1001,"shareName":"fixture"}`, true}, {"wrong_subscription", `{"subscriptionId":1002,"shareName":"fixture"}`, false}, {"missing_optional", `{"shareName":"fixture"}`, true}, {"recovery", `{"subscriptionId":1001,"shareName":"fixture"}`, true}}},
		{"active_active", "/subscriptions/1001/regions/7/private-link", &lookupv1alpha1.ActiveActivePrivateLink{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.ActiveActivePrivateLinkSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.SubscriptionRegionParameters{SubscriptionID: "1001", RegionID: "7"}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ActiveActivePrivateLinkReconciler{Client: c}).Reconcile
		}, func(o client.Object) (lookupv1alpha1.PrivateLinkObservation, xpv1.Condition) {
			v := o.(*lookupv1alpha1.ActiveActivePrivateLink)
			return v.Status.AtProvider, v.Status.GetCondition(xpv1.TypeReady)
		}, []identityStep{{"initial", `{"subscriptionId":1001,"regionId":7,"shareName":"fixture"}`, true}, {"wrong_subscription", `{"subscriptionId":1002,"regionId":7,"shareName":"fixture"}`, false}, {"wrong_region", `{"subscriptionId":1001,"regionId":8,"shareName":"fixture"}`, false}, {"missing_optional", `{"shareName":"fixture"}`, true}, {"recovery", `{"subscriptionId":1001,"regionId":7,"shareName":"fixture"}`, true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			bodies := map[string]taskBody{tc.path: {task: identityLinkTask, resource: tc.steps[0].resource}}
			handler := taskRoutes(t, &calls, bodies)
			kube := lookupClient(t, onlyGET(t, handler), tc.obj)
			run := tc.run(kube)
			for _, step := range tc.steps {
				t.Run(step.name, func(t *testing.T) {
					bodies[tc.path] = taskBody{task: identityLinkTask, resource: step.resource}
					result, err := run(context.Background(), request(name))
					if err != nil {
						t.Fatal(err)
					}
					wantDelay := requeue
					if !step.ready {
						wantDelay = requeueFailed
					}
					if result.RequeueAfter != wantDelay {
						t.Fatalf("requeue=%s", result.RequeueAfter)
					}
					if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, tc.obj); err != nil {
						t.Fatal(err)
					}
					observed, ready := tc.status(tc.obj)
					if step.ready {
						if ready.Status != corev1.ConditionTrue || observed.ShareName != "fixture" {
							t.Fatalf("status=%+v observation=%+v", ready, observed)
						}
					} else if ready.Status != corev1.ConditionFalse || !reflect.DeepEqual(observed, lookupv1alpha1.PrivateLinkObservation{}) || ready.Message != "lookup failed; check provider configuration and Redis Cloud access" {
						t.Fatalf("failure not safely cleared: %+v %+v", ready, observed)
					}
				})
			}
		})
	}
}

type identityStep struct {
	name, resource string
	ready          bool
}

func onlyGET(t *testing.T, next http.Handler) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestPrivateLinkScriptParentIdentity(t *testing.T) { //nolint:gocyclo // Verify each lookup and no downstream request on contradiction.
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("hosted Actions only")
	}
	const name = "fixture"
	ref := xpv1.Reference{Name: exampleProviderConfig}
	obj := &lookupv1alpha1.PrivateLinkEndpointScript{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PrivateLinkEndpointScriptSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "1001"}}}
	calls := []string{}
	bodies := map[string]taskBody{
		"/subscriptions/1001":                              {direct: `{"id":1001}`},
		"/subscriptions/1001/private-link":                 {task: identityLinkTask, resource: `{"subscriptionId":1001,"status":"active"}`},
		"/subscriptions/1001/private-link/endpoint-script": {task: "script", resource: `{"resourceEndpointScript":"fixture-script"}`},
	}
	kube := lookupClient(t, onlyGET(t, taskRoutes(t, &calls, bodies)), obj)
	rec := &PrivateLinkEndpointScriptReconciler{Client: kube}
	for _, step := range []struct {
		name, sub, link string
		ready           bool
		wantLink        bool
	}{
		{"initial", `{"id":1001}`, `{"subscriptionId":1001,"status":"active"}`, true, true},
		{"wrong_subscription", `{"id":1002}`, `{"subscriptionId":1001,"status":"active"}`, false, false},
		{"wrong_link", `{"id":1001}`, `{"subscriptionId":1002,"status":"active"}`, false, true},
		{"missing_optional", `{}`, `{"status":"active"}`, true, true},
		{"recovery", `{"id":1001}`, `{"subscriptionId":1001,"status":"active"}`, true, true},
	} {
		t.Run(step.name, func(t *testing.T) {
			bodies["/subscriptions/1001"] = taskBody{direct: step.sub}
			bodies["/subscriptions/1001/private-link"] = taskBody{task: identityLinkTask, resource: step.link}
			before := len(calls)
			result, err := rec.Reconcile(context.Background(), request(name))
			if err != nil {
				t.Fatal(err)
			}
			gotCalls := strings.Join(calls[before:], "\n")
			hasLink := strings.Contains(gotCalls, "GET /subscriptions/1001/private-link\n")
			hasScript := strings.Contains(gotCalls, "/endpoint-script")
			if hasLink != step.wantLink || hasScript != step.ready {
				t.Fatalf("unexpected calls: %s", gotCalls)
			}
			delay := requeue
			if !step.ready {
				delay = requeueFailed
			}
			if result.RequeueAfter != delay {
				t.Fatalf("requeue=%s", result.RequeueAfter)
			}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, obj); err != nil {
				t.Fatal(err)
			}
			ready := obj.Status.GetCondition(xpv1.TypeReady)
			if step.ready {
				if ready.Status != corev1.ConditionTrue || obj.Status.AtProvider.EndpointScript != "fixture-script" {
					t.Fatalf("unexpected status: %+v", obj.Status)
				}
			} else if ready.Status != corev1.ConditionFalse || !reflect.DeepEqual(obj.Status.AtProvider, lookupv1alpha1.PrivateLinkEndpointScriptObservation{}) || ready.Message != "lookup failed; check provider configuration and Redis Cloud access" {
				t.Fatalf("failure not safely cleared: %+v", obj.Status)
			}
		})
	}
}
