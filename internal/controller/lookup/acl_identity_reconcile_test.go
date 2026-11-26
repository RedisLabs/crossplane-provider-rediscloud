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
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestACLLookupIdentityValidationAndRecovery(t *testing.T) { //nolint:gocyclo // Each lookup and response transition is checked explicitly.
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("hosted Actions only")
	}
	const name = "fixture"
	ref := xpv1.Reference{Name: exampleProviderConfig}
	for _, tc := range []struct {
		name, path, list string
		obj              client.Object
		reconcile        func(client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error)
		status           func(client.Object) (string, xpv1.Condition, bool)
	}{
		{"role", "/acl/roles", "roles", &lookupv1alpha1.ACLRole{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.ACLRoleSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ACLRoleParameters{Name: name}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ACLRoleReconciler{Client: c}).Reconcile
		}, func(o client.Object) (string, xpv1.Condition, bool) {
			v := o.(*lookupv1alpha1.ACLRole)
			return v.Status.AtProvider.ID, v.Status.GetCondition(xpv1.TypeReady), reflect.DeepEqual(v.Status.AtProvider, lookupv1alpha1.ACLRoleObservation{})
		}},
		{"rule", "/acl/redisRules", "redisRules", &lookupv1alpha1.ACLRule{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.ACLRuleSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ACLRuleParameters{Name: name}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ACLRuleReconciler{Client: c}).Reconcile
		}, func(o client.Object) (string, xpv1.Condition, bool) {
			v := o.(*lookupv1alpha1.ACLRule)
			return v.Status.AtProvider.ID, v.Status.GetCondition(xpv1.TypeReady), reflect.DeepEqual(v.Status.AtProvider, lookupv1alpha1.ACLRuleObservation{})
		}},
		{"user", "/acl/users", "users", &lookupv1alpha1.ACLUser{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.ACLUserSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ACLUserParameters{Name: name}}}, func(c client.Client) func(context.Context, reconcile.Request) (reconcile.Result, error) {
			return (&ACLUserReconciler{Client: c}).Reconcile
		}, func(o client.Object) (string, xpv1.Condition, bool) {
			v := o.(*lookupv1alpha1.ACLUser)
			return v.Status.AtProvider.ID, v.Status.GetCondition(xpv1.TypeReady), reflect.DeepEqual(v.Status.AtProvider, lookupv1alpha1.ACLUserObservation{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idField := `,"id":42`
			calls := 0
			kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusBadRequest)
					return
				}
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"%s":[{"name":"fixture"%s}]}`, tc.list, idField)
			}), tc.obj)
			run := tc.reconcile(kube)
			for _, step := range []struct {
				name, idField, wantID string
				ready                 corev1.ConditionStatus
				delay                 bool
			}{
				{"initial", `,"id":42`, "42", corev1.ConditionTrue, false},
				{"missing", "", "", corev1.ConditionFalse, true},
				{"zero", `,"id":0`, "", corev1.ConditionFalse, true},
				{"negative", `,"id":-1`, "", corev1.ConditionFalse, true},
				{"recovery", `,"id":43`, "43", corev1.ConditionTrue, false},
			} {
				t.Run(step.name, func(t *testing.T) {
					idField = step.idField
					before := calls
					result, err := run(context.Background(), request(name))
					if err != nil || calls != before+1 {
						t.Fatalf("reconcile: calls=%d result=%+v err=%v", calls-before, result, err)
					}
					wantDelay := requeue
					if step.delay {
						wantDelay = requeueFailed
					}
					if result.RequeueAfter != wantDelay {
						t.Fatalf("requeue=%s want=%s", result.RequeueAfter, wantDelay)
					}
					if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, tc.obj); err != nil {
						t.Fatal(err)
					}
					id, ready, cleared := tc.status(tc.obj)
					if id != step.wantID || ready.Status != step.ready {
						t.Fatalf("id=%q ready=%+v", id, ready)
					}
					if step.delay && !cleared {
						t.Fatal("stale observation was not fully cleared")
					}
					if step.delay && ready.Message != "lookup failed; check provider configuration and Redis Cloud access" {
						t.Fatalf("unsafe failure condition: %q", ready.Message)
					}
				})
			}
		})
	}
}
