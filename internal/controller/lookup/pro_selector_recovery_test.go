// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestProSubscriptionSelectorChangeRecovery(t *testing.T) { //nolint:gocyclo // Assert the complete selector transition and stale-status boundary.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	var response atomic.Int32
	var calls atomic.Int32
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret || (r.URL.Path != "/subscriptions/123" && r.URL.Path != "/subscriptions/124") {
			t.Error("unexpected fake request")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch response.Load() {
		case 0:
			if r.URL.Path != "/subscriptions/123" {
				t.Error("wrong initial path")
			}
			_, _ = io.WriteString(w, `{"id":123,"name":"first","status":"active","deploymentType":"single-region"}`)
		case 1:
			if r.URL.Path != "/subscriptions/124" {
				t.Error("wrong changed path")
			}
			_, _ = io.WriteString(w, `{"id":123,"name":"old","status":"active","deploymentType":"single-region"}`)
		case 2:
			if r.URL.Path != "/subscriptions/124" {
				t.Error("wrong recovery path")
			}
			_, _ = io.WriteString(w, `{"id":124,"name":"second","status":"active","deploymentType":"single-region"}`)
		default:
			t.Error("unexpected response phase")
			http.Error(w, "forbidden", http.StatusForbidden)
		}
	}), &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}})
	rec := &ProSubscriptionReconciler{Client: kube}
	current := &lookupv1alpha1.ProSubscription{}
	reconcile := func(t *testing.T, id string, ready bool) {
		t.Helper()
		before := calls.Load()
		result, err := rec.Reconcile(context.Background(), request(proFixtureName))
		if err != nil || calls.Load() != before+1 {
			t.Fatalf("reconcile: calls=%d err=%v", calls.Load()-before, err)
		}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, current); err != nil {
			t.Fatal(err)
		}
		condition := current.Status.GetCondition(xpv1.TypeReady)
		if !ready {
			if result.RequeueAfter != requeueFailed || condition.Status != corev1.ConditionFalse || condition.Message != fixtureLookupSafeError || current.Status.AtProvider != (lookupv1alpha1.ProSubscriptionObservation{}) {
				t.Fatalf("stale observation after failure: %#v", current.Status)
			}
			return
		}
		if result.RequeueAfter != requeue || condition.Status != corev1.ConditionTrue || current.Status.AtProvider.ID != id || current.Status.AtProvider.Status != "active" || current.Status.AtProvider.DeploymentType != "single-region" {
			t.Fatalf("unexpected observation: %#v", current.Status)
		}
	}
	t.Run("initial", func(t *testing.T) { reconcile(t, "123", true) })
	current.Spec.ForProvider.SubscriptionID = "124"
	if err := kube.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	response.Store(1)
	t.Run("wrong_identity_clears", func(t *testing.T) { reconcile(t, "", false) })
	response.Store(2)
	t.Run("recovery", func(t *testing.T) { reconcile(t, "124", true) })
}
