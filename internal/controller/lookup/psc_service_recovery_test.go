// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"errors"
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

const fixturePSCStaleID = "stale"

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
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"resource":{"id":7,"status":"active"}}}`)
			case 1:
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"resource":{"status":"active"}}}`)
			case 2:
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"resource":{"id":0,"status":"active"}}}`)
			case 3:
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"resource":{"id":8,"status":"active"}}}`)
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

// Exercise the actual reconciler and pinned SDK using malformed fake task responses.
func TestPSCServiceMalformedTaskRecovery(t *testing.T) { //nolint:gocyclo // Keep both malformed shapes, redaction and recovery together.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	var phase atomic.Int32
	var serviceCalls, taskCalls atomic.Int32
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
			t.Error("unexpected fake request")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/subscriptions/123/private-service-connect":
			serviceCalls.Add(1)
			if phase.Load() == 1 {
				_, _ = io.WriteString(w, `{"sensitive":"sentinel-secret"}`)
			} else {
				_, _ = io.WriteString(w, `{"taskId":"fixture-task"}`)
			}
		case "/tasks/fixture-task":
			taskCalls.Add(1)
			if phase.Load() == 3 {
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"sensitive":"sentinel-secret"}}`)
			} else {
				_, _ = io.WriteString(w, `{"status":"processing-completed","response":{"resource":{"id":7,"status":"active"}}}`)
			}
		default:
			t.Error("unexpected fake path")
			http.Error(w, "forbidden", http.StatusForbidden)
		}
	}), &lookupv1alpha1.PrivateServiceConnect{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.PrivateServiceConnectSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}})
	rec := &PrivateServiceConnectReconciler{Client: kube}
	for _, tc := range []struct {
		name  string
		phase int32
		ready bool
	}{{"initial", 0, true}, {"missing_task_id", 1, false}, {"recovery_one", 2, true}, {"missing_resource", 3, false}, {"recovery_two", 4, true}} {
		t.Run(tc.name, func(t *testing.T) {
			phase.Store(tc.phase)
			beforeService, beforeTask := serviceCalls.Load(), taskCalls.Load()
			result, err := rec.Reconcile(context.Background(), request(proFixtureName))
			taskDelta := int32(1)
			if tc.phase == 1 {
				taskDelta = 0
			}
			if err != nil || serviceCalls.Load() != beforeService+1 || taskCalls.Load() != beforeTask+taskDelta {
				t.Fatalf("unexpected reconciliation or request counts: %v", err)
			}
			got := &lookupv1alpha1.PrivateServiceConnect{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, got); err != nil {
				t.Fatal(err)
			}
			cond := got.Status.GetCondition(xpv1.TypeReady)
			if !tc.ready {
				if result.RequeueAfter != requeueFailed || cond.Status != corev1.ConditionFalse || cond.Message != fixtureLookupSafeError || got.Status.AtProvider != (lookupv1alpha1.PrivateServiceConnectObservation{}) {
					t.Fatalf("unsafe failed status: %#v", got.Status)
				}
				return
			}
			if result.RequeueAfter != requeue || cond.Status != corev1.ConditionTrue || got.Status.AtProvider.PrivateServiceConnectServiceID != "7" {
				t.Fatalf("unexpected recovered status: %#v", got.Status)
			}
		})
	}
}

func TestSafePSCReadDiscardsPanic(t *testing.T) {
	result, err := safePSCRead(func() (*string, error) { panic("sentinel-secret") })
	if result != nil || err == nil || err.Error() != "private service connect lookup failed" {
		t.Fatal("panic was not sanitized")
	}
}

func TestSafePSCReadPreservesError(t *testing.T) {
	original := errors.New("ordinary error")
	result, err := safePSCRead(func() (*string, error) { return nil, original })
	if result != nil || err != original {
		t.Fatal("ordinary error changed")
	}
}
