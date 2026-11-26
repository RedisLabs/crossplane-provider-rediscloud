// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPaymentMethodPartialIdentity(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("hosted Actions only")
	}
	var body atomic.Value
	var calls atomic.Int32
	body.Store("")
	const name = "payment-partial"
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/payment-methods" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
			t.Error("unexpected fake request")
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body.Load().(string))
	}), &lookupv1alpha1.PaymentMethod{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PaymentMethodSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.PaymentMethodParameters{LastFourNumbers: stringPtr("0000")}}})
	rec := &PaymentMethodReconciler{Client: kube}
	for _, step := range []struct {
		name, response string
		ready          bool
		id             string
	}{
		{"zero_id", `{"paymentMethods":[{"id":0,"creditCardEndsWith":0,"expirationMonth":12,"expirationYear":2099}]}`, true, "0"},
		{"missing_digits", `{"paymentMethods":[{"id":1,"expirationMonth":12,"expirationYear":2099}]}`, false, ""},
		{"after_missing_digits", `{"paymentMethods":[{"id":0,"creditCardEndsWith":0,"expirationMonth":12,"expirationYear":2099}]}`, true, "0"},
		{"ambiguous_digits", `{"paymentMethods":[{"id":0,"creditCardEndsWith":0,"expirationMonth":12,"expirationYear":2099},{"id":1,"expirationMonth":12,"expirationYear":2099}]}`, false, ""},
		{"after_ambiguous_digits", `{"paymentMethods":[{"id":0,"creditCardEndsWith":0,"expirationMonth":12,"expirationYear":2099}]}`, true, "0"},
		{"missing_id", `{"paymentMethods":[{"creditCardEndsWith":0,"expirationMonth":12,"expirationYear":2099}]}`, false, ""},
		{"recovery", `{"paymentMethods":[{"id":2,"creditCardEndsWith":0,"expirationMonth":12,"expirationYear":2099}]}`, true, "2"},
	} {
		t.Run(step.name, func(t *testing.T) {
			body.Store(step.response)
			before := calls.Load()
			result, err := rec.Reconcile(context.Background(), request(name))
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != before+1 {
				t.Fatal("expected exactly one fake GET")
			}
			got := &lookupv1alpha1.PaymentMethod{}
			if err = kube.Get(context.Background(), types.NamespacedName{Name: name}, got); err != nil {
				t.Fatal(err)
			}
			ready := got.Status.GetCondition(xpv1.TypeReady)
			if step.ready {
				if result.RequeueAfter != requeue || ready.Status != corev1.ConditionTrue || got.Status.AtProvider.ID != step.id || got.Status.AtProvider.LastFourNumbers != "0000" {
					t.Fatal("expected exact ready payment identity")
				}
				return
			}
			if result.RequeueAfter != requeueFailed || ready.Status != corev1.ConditionFalse || ready.Message != fixtureLookupSafeError || !reflect.DeepEqual(got.Status.AtProvider, lookupv1alpha1.PaymentMethodObservation{}) {
				t.Fatal("partial identity did not clear stale status")
			}
		})
	}
}
