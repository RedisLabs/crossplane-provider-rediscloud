// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPSCServiceLookupIdentityRecovery(t *testing.T) { //nolint:gocyclo // Keep task, identity and recovery checks together.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	var phase atomic.Int32
	var serviceCalls, taskCalls atomic.Int32
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
			t.Error("unexpected or unauthenticated fake request")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/subscriptions/123/private-service-connect":
			serviceCalls.Add(1)
			_, _ = io.WriteString(w, `{"taskId":"fixture-task"}`)
		case "/tasks/fixture-task":
			taskCalls.Add(1)
			switch phase.Load() {
			case 0:
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"id":7,"status":"active"}}`)
			case 1:
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"status":"active"}}`)
			case 2:
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"id":0,"status":"active"}}`)
			case 3:
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"id":8,"status":"active"}}`)
			default:
				t.Error("unexpected task phase")
				http.Error(w, "forbidden", http.StatusForbidden)
			}
		default:
			t.Error("unexpected fake route")
			http.Error(w, "forbidden", http.StatusForbidden)
		}
	}), &lookupv1alpha1.PrivateServiceConnect{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.PrivateServiceConnectSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}})
	rec := &PrivateServiceConnectReconciler{Client: kube}
	for _, tc := range []struct {
		name, wantID string
		phase        int32
		ready        bool
	}{
		{"initial", "7", 0, true}, {"missing_id", "", 1, false}, {"zero_id", "0", 2, true}, {"recovery", "8", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			phase.Store(tc.phase)
			beforeService, beforeTask := serviceCalls.Load(), taskCalls.Load()
			result, err := rec.Reconcile(context.Background(), request(proFixtureName))
			if err != nil || serviceCalls.Load() != beforeService+1 || taskCalls.Load() != beforeTask+1 {
				t.Fatalf("unexpected calls: service=%d task=%d err=%v", serviceCalls.Load()-beforeService, taskCalls.Load()-beforeTask, err)
			}
			current := &lookupv1alpha1.PrivateServiceConnect{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, current); err != nil {
				t.Fatal(err)
			}
			condition := current.Status.GetCondition(xpv1.TypeReady)
			if !tc.ready {
				if result.RequeueAfter != requeueFailed || condition.Status != corev1.ConditionFalse || condition.Message != fixtureLookupSafeError || current.Status.AtProvider != (lookupv1alpha1.PrivateServiceConnectObservation{}) {
					t.Fatalf("failure retained observation: %#v", current.Status)
				}
				return
			}
			if result.RequeueAfter != requeue || condition.Status != corev1.ConditionTrue || current.Status.AtProvider.PrivateServiceConnectServiceID != tc.wantID || current.Status.AtProvider.Status != "active" {
				t.Fatalf("unexpected observation: %#v", current.Status)
			}
		})
	}
}

// Reproduces the pinned SDK's nil task-ID dereference; this is not recovery proof.
func TestPSCServiceLookupMissingTaskIDPanics(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	var calls atomic.Int32
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/subscriptions/123/private-service-connect" || r.URL.RawQuery != "" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
			t.Error("unexpected or unauthenticated fake request")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}), &lookupv1alpha1.PrivateServiceConnect{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.PrivateServiceConnectSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}, Status: lookupv1alpha1.PrivateServiceConnectStatus{AtProvider: lookupv1alpha1.PrivateServiceConnectObservation{ID: "stale"}}})
	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		_, _ = (&PrivateServiceConnectReconciler{Client: kube}).Reconcile(context.Background(), request(proFixtureName))
	}()
	if panicValue == nil || !strings.Contains(fmt.Sprint(panicValue), "invalid memory address or nil pointer dereference") || calls.Load() != 1 {
		t.Fatalf("missing task ID did not reproduce panic: calls=%d", calls.Load())
	}
	current := &lookupv1alpha1.PrivateServiceConnect{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.AtProvider.ID != "stale" {
		t.Fatal("panic unexpectedly cleared stale observation")
	}
}
