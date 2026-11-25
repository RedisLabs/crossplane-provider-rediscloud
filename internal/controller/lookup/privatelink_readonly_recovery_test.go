// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
)

// Exercise the actual lookup reconciler and SDK against an authenticated
// loopback API. Failed refresh must clear stale observation, recover on the
// next read, and never issue a Cloud mutation.
func TestPrivateLinkReadOnlyFailureAndRecovery(t *testing.T) {
	for _, failure := range []string{"forbidden", "unavailable", "task-error"} {
		t.Run(failure, func(t *testing.T) {
			var mu sync.Mutex
			phase := "healthy"
			var calls []string
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.Method != http.MethodGet || r.Header.Get("X-Api-Key") != "key" || r.Header.Get("X-Api-Secret-Key") != "secret" {
					t.Errorf("unexpected method or credentials on %s", r.URL.Path)
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/subscriptions/1001/private-link":
					if phase == "forbidden" {
						http.Error(w, "fixture unavailable", http.StatusForbidden)
						return
					}
					if phase == "unavailable" {
						http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
						return
					}
					_, _ = fmt.Fprint(w, `{"taskId":"link"}`)
				case "/tasks/link":
					if phase == "task-error" {
						_, _ = fmt.Fprint(w, `{"taskId":"link","status":"processing-error","description":"fixture failure"}`)
						return
					}
					_, _ = fmt.Fprint(w, `{"taskId":"link","status":"processing-completed","response":{"resource":{"shareName":"fixture-share","resourceConfigurationId":"fixture-resource"}}}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			item := &lookupv1alpha1.PrivateLink{
				ObjectMeta: metav1.ObjectMeta{Name: "fixture-link", Finalizers: []string{"test.example/finalizer"}},
				Spec:       lookupv1alpha1.PrivateLinkSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "1001"}},
			}
			kube := lookupClient(t, handler, item)
			reconciler := &PrivateLinkReconciler{Client: kube}
			reconcile := func(expected time.Duration) *lookupv1alpha1.PrivateLink {
				t.Helper()
				result, err := reconciler.Reconcile(context.Background(), request(item.Name))
				if err != nil || result.RequeueAfter != expected {
					t.Fatalf("reconcile result=%v error=%v", result, err)
				}
				got := &lookupv1alpha1.PrivateLink{}
				if err := kube.Get(context.Background(), types.NamespacedName{Name: item.Name}, got); err != nil {
					t.Fatal(err)
				}
				return got
			}
			got := reconcile(requeue)
			if got.Status.AtProvider.ShareName != "fixture-share" || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) {
				t.Fatalf("initial status = %#v", got.Status)
			}
			mu.Lock()
			phase = failure
			mu.Unlock()
			got = reconcile(requeueFailed)
			if got.Status.AtProvider.ShareName != "" || got.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionFalse {
				t.Fatalf("failure retained stale data: %#v", got.Status)
			}
			mu.Lock()
			phase = "healthy"
			mu.Unlock()
			got = reconcile(requeue)
			if got.Status.AtProvider.ShareName != "fixture-share" || got.Status.AtProvider.ResourceConfigurationID != "fixture-resource" || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) {
				t.Fatalf("recovery status = %#v", got.Status)
			}
			if err := kube.Delete(context.Background(), got); err != nil {
				t.Fatal(err)
			}
			deleting := &lookupv1alpha1.PrivateLink{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: item.Name}, deleting); err != nil || deleting.DeletionTimestamp.IsZero() {
				t.Fatalf("expected terminating lookup: object=%#v err=%v", deleting, err)
			}
			mu.Lock()
			before := len(calls)
			mu.Unlock()
			if _, err := reconciler.Reconcile(context.Background(), request(item.Name)); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(calls) != before {
				t.Fatalf("lookup deletion called Cloud: %v", calls[before:])
			}
			for _, call := range calls {
				if !strings.HasPrefix(call, "GET ") {
					t.Fatalf("mutation observed: %s", call)
				}
			}
		})
	}
}
