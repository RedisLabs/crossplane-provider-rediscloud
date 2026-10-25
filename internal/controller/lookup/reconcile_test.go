// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/apis/v1beta1"
)

func TestPaymentMethodReconcileWritesID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/payment-methods" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "key" || r.Header.Get("X-Api-Secret-Key") != "secret" {
			t.Error("credentials were not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"paymentMethods":[{"id":42,"type":"Visa","creditCardEndsWith":4242,"expirationMonth":12,"expirationYear":2099},{"id":7,"type":"Mastercard","creditCardEndsWith":1111,"expirationMonth":1,"expirationYear":2099}]}`)
	}))
	t.Cleanup(server.Close)

	kube := fakeClient(t, server.URL, &lookupv1alpha1.PaymentMethod{
		ObjectMeta: metav1.ObjectMeta{Name: "example-payment-method"},
		Spec: lookupv1alpha1.PaymentMethodSpec{
			ProviderConfigRef: xpv1.Reference{Name: "default"},
			ForProvider: lookupv1alpha1.PaymentMethodParameters{
				CardType: stringPtr("Visa"),
			},
		},
	})
	result, err := (&PaymentMethodReconciler{Client: kube}).Reconcile(context.Background(), request("example-payment-method"))
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("successful lookup did not requeue")
	}
	got := &lookupv1alpha1.PaymentMethod{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-payment-method"}, got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(lookupv1alpha1.PaymentMethodObservation{
		ID:              "42",
		CardType:        "Visa",
		LastFourNumbers: "4242",
	}, got.Status.AtProvider); diff != "" {
		t.Fatalf("status mismatch (-want +got):\n%s", diff)
	}
	if !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) {
		t.Fatalf("Ready = %#v", got.Status.GetCondition(xpv1.TypeReady))
	}
}

func TestPaymentMethodReconcileHidesHTMLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<html><title>500</title></html>", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	kube := fakeClient(t, server.URL, &lookupv1alpha1.PaymentMethod{
		ObjectMeta: metav1.ObjectMeta{Name: "example-payment-method"},
		Spec: lookupv1alpha1.PaymentMethodSpec{
			ProviderConfigRef: xpv1.Reference{Name: "default"},
		},
	})
	result, err := (&PaymentMethodReconciler{Client: kube}).Reconcile(context.Background(), request("example-payment-method"))
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("failed lookup did not requeue")
	}
	got := &lookupv1alpha1.PaymentMethod{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-payment-method"}, got); err != nil {
		t.Fatal(err)
	}
	message := got.Status.GetCondition(xpv1.TypeReady).Message
	if message == "" || strings.Contains(strings.ToLower(message), "<html") {
		t.Fatalf("Ready message = %q", message)
	}
	if got.Status.AtProvider.ID != "" {
		t.Fatalf("id = %s", got.Status.AtProvider.ID)
	}
}

func fakeClient(t *testing.T, apiURL string, payment *lookupv1alpha1.PaymentMethod) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		v1beta1.SchemeBuilder.AddToScheme,
		lookupv1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "crossplane-system"},
		Data: map[string][]byte{
			"credentials": []byte(`{"api_key":"key","secret_key":"secret","url":"` + apiURL + `"}`),
		},
	}
	config := &v1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: v1beta1.ProviderConfigSpec{
			Credentials: v1beta1.ProviderCredentials{
				Source: xpv1.CredentialsSourceSecret,
				CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
					SecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "creds",
							Namespace: "crossplane-system",
						},
						Key: "credentials",
					},
				},
			},
		},
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(payment).
		WithObjects(secret, config, payment).
		Build()
}

func request(name string) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}
}

func stringPtr(value string) *string {
	return &value
}
