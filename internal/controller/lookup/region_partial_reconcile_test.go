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

const fixturePartialRegionsName = "regions"

func TestRegionPartialResponseRecovery(t *testing.T) { //nolint:gocyclo // Each response transition is asserted.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	const good = `{"id":11,"name":"us-east-1","provider":"AWS"}`
	var rows atomic.Value
	rows.Store(good)
	var calls atomic.Int32
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/regions" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
			t.Error("unexpected fake request")
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"regions":[`+rows.Load().(string)+`]}`)
	}), &lookupv1alpha1.Regions{ObjectMeta: metav1.ObjectMeta{Name: fixturePartialRegionsName}, Spec: lookupv1alpha1.RegionsSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.RegionsParameters{ProviderName: stringPtr("AWS"), Name: stringPtr("us-east-1")}}})
	rec := &RegionsReconciler{Client: kube}
	for _, tc := range []struct {
		name, body, id string
		ok             bool
	}{
		{fixtureLookupInitial, good, "11", true},
		{"null_row", good + `,null`, "", false},
		{"after_null", good, "11", true},
		{"missing_provider", good + `,{"id":12,"name":"us-east-1"}`, "", false},
		{"after_provider", good, "11", true},
		{"missing_name", good + `,{"id":12,"provider":"AWS"}`, "", false},
		{"after_name", good, "11", true},
		{"missing_id", `{"name":"us-east-1","provider":"AWS"}`, "", false},
		{"after_id", good, "11", true},
		{"known_mismatch", good + `,{"id":12,"provider":"GCP"},{"id":13,"name":"other"}`, "11", true},
		{"zero_id", `{"id":0,"name":"us-east-1","provider":"AWS"}`, "0", true},
		{"recovery", good, "11", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows.Store(tc.body)
			before := calls.Load()
			result, err := rec.Reconcile(context.Background(), request(fixturePartialRegionsName))
			if err != nil || calls.Load() != before+1 {
				t.Fatalf("reconcile failed: %v, calls=%d", err, calls.Load()-before)
			}
			obj := &lookupv1alpha1.Regions{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: fixturePartialRegionsName}, obj); err != nil {
				t.Fatal(err)
			}
			ready := obj.Status.GetCondition(xpv1.TypeReady)
			if !tc.ok {
				if result.RequeueAfter != requeueFailed || ready.Status != corev1.ConditionFalse || len(obj.Status.AtProvider.Regions) != 0 || ready.Message != fixtureLookupSafeError {
					t.Fatalf("failure did not clear status: %#v", obj.Status)
				}
				return
			}
			if result.RequeueAfter != requeue || ready.Status != corev1.ConditionTrue || len(obj.Status.AtProvider.Regions) != 1 || obj.Status.AtProvider.Regions[0].RegionID != tc.id {
				t.Fatalf("unexpected status: %#v", obj.Status)
			}
		})
	}
}

func TestRegionSelectedObservationFields(t *testing.T) { //nolint:gocyclo // Selected-field cases retain explicit status assertions.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	for _, tc := range []struct {
		name, body           string
		provider, regionName *string
		ok                   bool
	}{
		{"missing_name_unfiltered", `{"id":1,"provider":"AWS"}`, nil, nil, false},
		{"missing_name_provider_only", `{"id":1,"provider":"AWS"}`, stringPtr("AWS"), nil, false},
		{"missing_provider_unfiltered", `{"id":1,"name":"us-east-1"}`, nil, nil, false},
		{"missing_provider_name_only", `{"id":1,"name":"us-east-1"}`, nil, stringPtr("us-east-1"), false},
		{"explicit_empty_fields", `{"id":0,"name":"","provider":""}`, nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/regions" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
					t.Error("unexpected fake request")
					http.Error(w, "unexpected", http.StatusBadRequest)
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"regions":[`+tc.body+`]}`)
			}), &lookupv1alpha1.Regions{ObjectMeta: metav1.ObjectMeta{Name: fixturePartialRegionsName}, Spec: lookupv1alpha1.RegionsSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.RegionsParameters{ProviderName: tc.provider, Name: tc.regionName}}})
			rec := &RegionsReconciler{Client: kube}
			result, err := rec.Reconcile(context.Background(), request(fixturePartialRegionsName))
			if err != nil || calls.Load() != 1 {
				t.Fatalf("reconcile: %v; calls=%d", err, calls.Load())
			}
			obj := &lookupv1alpha1.Regions{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: fixturePartialRegionsName}, obj); err != nil {
				t.Fatal(err)
			}
			ready := obj.Status.GetCondition(xpv1.TypeReady)
			if !tc.ok {
				if result.RequeueAfter != requeueFailed || ready.Status != corev1.ConditionFalse || len(obj.Status.AtProvider.Regions) != 0 || ready.Message != fixtureLookupSafeError {
					t.Fatalf("unexpected failure status: %#v", obj.Status)
				}
				return
			}
			if result.RequeueAfter != requeue || ready.Status != corev1.ConditionTrue || len(obj.Status.AtProvider.Regions) != 1 || obj.Status.AtProvider.Regions[0].RegionID != "0" {
				t.Fatalf("unexpected status: %#v", obj.Status)
			}
		})
	}
}
