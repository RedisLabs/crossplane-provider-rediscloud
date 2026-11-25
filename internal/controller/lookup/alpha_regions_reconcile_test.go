// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAlphaRegionLookup(t *testing.T) { //nolint:gocyclo // Keep the GET-only request, result and stale-status assertions together.
	for _, tc := range []struct{ provider, name, id string }{
		{"AWS", "us-east-1", "11"}, {"GCP", "us-central1", "22"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			var calls atomic.Int32
			var empty atomic.Bool
			kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/regions" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, "GET regions only", http.StatusMethodNotAllowed)
					return
				}
				if r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
					t.Error("dummy credentials were not sent")
					http.Error(w, "authentication required", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if empty.Load() {
					_, _ = io.WriteString(w, `{"regions":[]}`)
					return
				}
				_, _ = io.WriteString(w, `{"regions":[{"id":11,"name":"us-east-1","provider":"AWS"},{"id":22,"name":"us-central1","provider":"GCP"}]}`)
			}), &lookupv1alpha1.Regions{
				ObjectMeta: metav1.ObjectMeta{Name: "alpha-regions"},
				Spec:       lookupv1alpha1.RegionsSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.RegionsParameters{ProviderName: stringPtr(tc.provider), Name: stringPtr(tc.name)}},
			})
			reconciler := &RegionsReconciler{Client: kube}
			if err := reconcileReady(t, reconciler.Reconcile, "alpha-regions"); err != nil {
				t.Fatal(err)
			}
			got := &lookupv1alpha1.Regions{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: "alpha-regions"}, got); err != nil {
				t.Fatal(err)
			}
			want := []lookupv1alpha1.RegionObservation{{RegionID: tc.id, Name: tc.name, ProviderName: tc.provider}}
			if diff := cmp.Diff(want, got.Status.AtProvider.Regions); diff != "" || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) {
				t.Fatalf("lookup status mismatch: %s; condition=%v", diff, got.Status.GetCondition(xpv1.TypeReady))
			}
			empty.Store(true)
			result, err := reconciler.Reconcile(context.Background(), request("alpha-regions"))
			if err != nil || result.RequeueAfter != requeueFailed {
				t.Fatalf("empty result: %s, %v", result.RequeueAfter, err)
			}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: "alpha-regions"}, got); err != nil {
				t.Fatal(err)
			}
			if len(got.Status.AtProvider.Regions) != 0 || got.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionFalse {
				t.Fatalf("empty result left stale region or Ready: %#v", got.Status)
			}
			if calls.Load() != 2 {
				t.Fatalf("expected two GET requests, got %d", calls.Load())
			}
		})
	}
}
