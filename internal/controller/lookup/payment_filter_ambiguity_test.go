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
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPaymentMethodFilterAmbiguity(t *testing.T) { //nolint:gocyclo // Keep the ordered stale-state and recovery assertions together.
	if os.Getenv("GITHUB_ACTIONS") != fixtureActionsEnabled {
		t.Skip("hosted Actions only")
	}
	var body atomic.Value
	var calls atomic.Int32
	body.Store("")
	const name = "payment-filter-fixture"
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/payment-methods" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
			t.Error("unexpected fake request")
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body.Load().(string))
	}), &lookupv1alpha1.PaymentMethod{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PaymentMethodSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.PaymentMethodParameters{CardType: stringPtr("Visa"), LastFourNumbers: stringPtr("4242")}}})
	rec := &PaymentMethodReconciler{Client: kube}
	year := time.Now().UTC().Year()
	valid := fmt.Sprintf(`{"id":1,"type":"Visa","creditCardEndsWith":4242,"expirationMonth":12,"expirationYear":%d}`, year+2)
	for _, step := range []struct {
		name, rows     string
		exclude, ready bool
	}{
		{fixtureLookupInitial, valid, true, true},
		{"unknown_type", valid + `,{"id":2,"creditCardEndsWith":4242,"expirationMonth":12,"expirationYear":2099}`, true, false},
		{"after_type", valid, true, true},
		{"unknown_year", valid + `,{"id":2,"type":"Visa","creditCardEndsWith":4242,"expirationMonth":12}`, true, false},
		{"after_year", valid, true, true},
		{"unknown_month", valid + fmt.Sprintf(`,{"id":2,"type":"Visa","creditCardEndsWith":4242,"expirationYear":%d}`, year), true, false},
		{"after_month", valid, true, true},
		{"null_row", valid + `,null`, true, false},
		{"after_null", valid, true, true},
		{"known_type_excludes", valid + `,{"id":2,"type":"Mastercard"}`, true, true},
		{"known_suffix_excludes", valid + `,{"id":2,"creditCardEndsWith":1111}`, true, true},
		{"known_expiry_excludes", valid + `,{"id":2,"expirationYear":2000}`, true, true},
		{"expiry_disabled", `{"id":1,"type":"Visa","creditCardEndsWith":4242}`, false, true},
		{"future_year_no_month", fmt.Sprintf(`{"id":1,"type":"Visa","creditCardEndsWith":4242,"expirationYear":%d}`, year+2), true, true},
	} {
		t.Run(step.name, func(t *testing.T) {
			obj := &lookupv1alpha1.PaymentMethod{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, obj); err != nil {
				t.Fatal(err)
			}
			obj.Spec.ForProvider.ExcludeExpired = &step.exclude
			if err := kube.Update(context.Background(), obj); err != nil {
				t.Fatal(err)
			}
			body.Store(`{"paymentMethods":[` + step.rows + `]}`)
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
				if result.RequeueAfter != requeue || ready.Status != corev1.ConditionTrue || got.Status.AtProvider.ID != "1" || got.Status.AtProvider.LastFourNumbers != "4242" {
					t.Fatal("valid match unavailable")
				}
				return
			}
			if result.RequeueAfter != requeueFailed || ready.Status != corev1.ConditionFalse || ready.Message != fixtureLookupSafeError || !reflect.DeepEqual(got.Status.AtProvider, lookupv1alpha1.PaymentMethodObservation{}) {
				t.Fatal("ambiguous response retained status")
			}
		})
	}
}
