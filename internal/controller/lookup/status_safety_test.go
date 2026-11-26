// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"net/http"
	"strings"
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestLookupControllerErrorBodiesNeverReachStatus(t *testing.T) { //nolint:gocyclo // Exercise each distinct controller path and condition.
	const name = "subject"
	const sentinel = "fixture-sensitive-response"
	ref := xpv1.Reference{Name: exampleProviderConfig}
	id := lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}
	for _, tc := range []struct {
		name string
		obj  client.Object
	}{
		{name: "regions", obj: &lookupv1alpha1.Regions{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.RegionsSpec{ProviderConfigRef: ref}}},
		{name: "payment", obj: &lookupv1alpha1.PaymentMethod{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PaymentMethodSpec{ProviderConfigRef: ref}}},
		{name: "acl-role", obj: &lookupv1alpha1.ACLRole{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.ACLRoleSpec{ProviderConfigRef: ref, ForProvider: lookupv1alpha1.ACLRoleParameters{Name: "fixture"}}}},
		{name: "private-link", obj: &lookupv1alpha1.PrivateLink{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PrivateLinkSpec{ProviderConfigRef: ref, ForProvider: id}}},
		{name: "psc", obj: &lookupv1alpha1.PrivateServiceConnect{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PrivateServiceConnectSpec{ProviderConfigRef: ref, ForProvider: id}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"` + sentinel + `"}`))
			}), tc.obj)
			var result reconcile.Result
			var err error
			switch tc.obj.(type) {
			case *lookupv1alpha1.Regions:
				result, err = (&RegionsReconciler{Client: kube}).Reconcile(context.Background(), request(name))
			case *lookupv1alpha1.PaymentMethod:
				result, err = (&PaymentMethodReconciler{Client: kube}).Reconcile(context.Background(), request(name))
			case *lookupv1alpha1.ACLRole:
				result, err = (&ACLRoleReconciler{Client: kube}).Reconcile(context.Background(), request(name))
			case *lookupv1alpha1.PrivateLink:
				result, err = (&PrivateLinkReconciler{Client: kube}).Reconcile(context.Background(), request(name))
			case *lookupv1alpha1.PrivateServiceConnect:
				result, err = (&PrivateServiceConnectReconciler{Client: kube}).Reconcile(context.Background(), request(name))
			}
			if err != nil || result.RequeueAfter != requeueFailed || calls != 1 {
				t.Fatalf("lookup did not fail after one API call: calls=%d result=%+v err=%v", calls, result, err)
			}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, tc.obj); err != nil {
				t.Fatal(err)
			}
			var conditions []xpv1.Condition
			switch obj := tc.obj.(type) {
			case *lookupv1alpha1.Regions:
				conditions = obj.Status.Conditions
			case *lookupv1alpha1.PaymentMethod:
				conditions = obj.Status.Conditions
			case *lookupv1alpha1.ACLRole:
				conditions = obj.Status.Conditions
			case *lookupv1alpha1.PrivateLink:
				conditions = obj.Status.Conditions
			case *lookupv1alpha1.PrivateServiceConnect:
				conditions = obj.Status.Conditions
			}
			if len(conditions) < 2 {
				t.Fatal("failure conditions missing")
			}
			for _, condition := range conditions {
				if strings.Contains(condition.Message, sentinel) || condition.Message != fixtureLookupSafeError {
					t.Fatal("condition did not use the safe failure message")
				}
			}
		})
	}
}
