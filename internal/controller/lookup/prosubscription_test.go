// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"net/http"
	"testing"
	"time"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestProSubscriptionLookupReadOnlyAndFailure(t *testing.T) { //nolint:gocyclo // Exercise the same controller over success and invalid responses.
	for _, tt := range []struct {
		name, id, body string
		code           int
		ready          bool
		wantCalls      int
	}{
		{"pro", "123", `{"id":123,"name":"fixture","status":"active","deploymentType":"single-region"}`, 200, true, 1},
		{"forbidden", "123", `{}`, 403, false, 1},
		{"not-found", "123", `{}`, 404, false, 1},
		{"server-error", "123", `{}`, 500, false, 1},
		{"empty-response", "123", `{}`, 200, false, 1},
		{"wrong-id", "123", `{"id":124,"deploymentType":"single-region"}`, 200, false, 1},
		{"active-active", "123", `{"id":123,"deploymentType":"active-active"}`, 200, false, 1},
		{"unknown-type", "123", `{"id":123}`, 200, false, 1},
		{"invalid-id", "-1", `{}`, 200, false, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/subscriptions/123" || r.Header.Get("X-Api-Key") != "key" || r.Header.Get("X-Api-Secret-Key") != "secret" {
					t.Errorf("unexpected or unauthenticated request %s %s", r.Method, r.URL.Path)
					http.Error(w, "denied", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(tt.body))
			})
			obj := &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: "subject"}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: tt.id}}, Status: lookupv1alpha1.ProSubscriptionStatus{AtProvider: lookupv1alpha1.ProSubscriptionObservation{ID: "stale"}}}
			kube := lookupClient(t, h, obj)
			rec := &ProSubscriptionReconciler{Client: kube}
			result, err := rec.Reconcile(context.Background(), request("subject"))
			if err != nil {
				t.Fatal(err)
			}
			got := &lookupv1alpha1.ProSubscription{}
			if err = kube.Get(context.Background(), types.NamespacedName{Name: "subject"}, got); err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, tt.wantCalls)
			}
			if tt.ready {
				if result.RequeueAfter != requeue || got.Status.AtProvider.ID != "123" || got.Status.AtProvider.Name != "fixture" || got.Status.AtProvider.Status != "active" || got.Status.AtProvider.DeploymentType != "single-region" {
					t.Fatalf("unexpected ready status=%+v result=%+v", got.Status.AtProvider, result)
				}
			} else if result.RequeueAfter != requeueFailed || got.Status.AtProvider.ID != "" {
				t.Fatalf("failure did not clear stale status: %+v", got.Status.AtProvider)
			}
		})
	}
}

func TestProSubscriptionDeleteDoesNotReadCloud(t *testing.T) {
	now := metav1.NewTime(time.Now())
	obj := &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: "subject", DeletionTimestamp: &now, Finalizers: []string{"test"}}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}}
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected Cloud call %s", r.Method) }), obj)
	result, err := (&ProSubscriptionReconciler{Client: kube}).Reconcile(context.Background(), request("subject"))
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("delete result=%+v err=%v", result, err)
	}
}
