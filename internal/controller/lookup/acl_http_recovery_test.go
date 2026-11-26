// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"fmt"
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

const fixtureACLHTTPName = "fixture"

func TestACLHTTPFailureRecovery(t *testing.T) { //nolint:gocyclo // Assert both actual reconcilers across the full sequence.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	const sentinel = "fixture-sensitive-acl-error"
	ref := xpv1.Reference{Name: exampleProviderConfig}
	for _, tc := range []struct {
		name, path, list string
		obj              client.Object
		reconcile        func(client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error)
		status           func(client.Object) (string, bool, []xpv1.Condition)
	}{
		{"role", "/acl/roles", "roles", &lookupv1alpha1.ACLRole{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.ACLRoleSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ACLRoleParameters{Name: fixtureACLHTTPName}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ACLRoleReconciler{Client: c}).Reconcile
		}, func(o client.Object) (string, bool, []xpv1.Condition) {
			v := o.(*lookupv1alpha1.ACLRole)
			return v.Status.AtProvider.ID, reflect.DeepEqual(v.Status.AtProvider, lookupv1alpha1.ACLRoleObservation{}), v.Status.Conditions
		}},
		{"user", "/acl/users", "users", &lookupv1alpha1.ACLUser{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.ACLUserSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ACLUserParameters{Name: fixtureACLHTTPName}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ACLUserReconciler{Client: c}).Reconcile
		}, func(o client.Object) (string, bool, []xpv1.Condition) {
			v := o.(*lookupv1alpha1.ACLUser)
			return v.Status.AtProvider.ID, reflect.DeepEqual(v.Status.AtProvider, lookupv1alpha1.ACLUserObservation{}), v.Status.Conditions
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var code atomic.Int32
			var calls atomic.Int32
			code.Store(http.StatusOK)
			kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != tc.path || r.URL.RawQuery != "" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
					t.Error("unexpected or unauthenticated fake request")
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if status := int(code.Load()); status != http.StatusOK {
					http.Error(w, sentinel, status)
					return
				}
				_, _ = fmt.Fprintf(w, `{"%s":[{"id":42,"name":"fixture"}]}`, tc.list)
			}), tc.obj)
			run := tc.reconcile(kube)
			for _, step := range []struct {
				name   string
				status int
				ready  bool
			}{
				{"initial", http.StatusOK, true}, {"forbidden", http.StatusForbidden, false}, {"after_forbidden", http.StatusOK, true}, {"unavailable", http.StatusServiceUnavailable, false}, {"after_unavailable", http.StatusOK, true},
			} {
				t.Run(step.name, func(t *testing.T) {
					code.Store(int32(step.status))
					before := calls.Load()
					result, err := run(context.Background(), request(proFixtureName))
					if err != nil || calls.Load() != before+1 {
						t.Fatalf("unexpected calls=%d err=%v", calls.Load()-before, err)
					}
					if err := kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, tc.obj); err != nil {
						t.Fatal(err)
					}
					id, cleared, conditions := tc.status(tc.obj)
					if step.ready {
						if result.RequeueAfter != requeue || id != "42" {
							t.Fatalf("unexpected ready observation id=%q", id)
						}
					} else {
						if result.RequeueAfter != requeueFailed || !cleared {
							t.Fatal("HTTP failure retained stale observation")
						}
					}
					foundReady := false
					for _, c := range conditions {
						if strings.Contains(c.Message, sentinel) {
							t.Fatal("response body leaked into condition")
						}
						if c.Type == xpv1.TypeReady {
							foundReady = true
							want := corev1.ConditionFalse
							if step.ready {
								want = corev1.ConditionTrue
							}
							if c.Status != want || (!step.ready && c.Message != fixtureLookupSafeError) {
								t.Fatalf("unexpected Ready condition: %+v", c)
							}
						}
					}
					if !foundReady {
						t.Fatal("Ready condition missing")
					}
				})
			}
		})
	}
}
