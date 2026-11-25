// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/apis/v1beta1"
	"github.com/RedisLabs/provider-rediscloud/internal/clients"
	selector "github.com/RedisLabs/provider-rediscloud/internal/lookup"
	"github.com/RedisLabs/rediscloud-go-api/redis"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReadonlyPaymentTransportRejectsUnsafeRequests(t *testing.T) { //nolint:gocyclo // Check each disallowed route and mutation method before forwarding.
	const paymentURL = "https://qa.example.test/v1/payment-methods"
	var forwarded atomic.Int32
	guard := &regionReadGuard{origin: "qa.example.test", path: "/v1/payment-methods", next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		forwarded.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, raw := range []string{"https://qa.example.test/v1/regions", "https://qa.example.test/v1/subscriptions", "https://other.example.test/v1/payment-methods", "https://qa.example.test/v1/payment-methods?limit=1"} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("unexpected route was accepted")
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(method, paymentURL, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("mutation method was accepted")
		}
	}
	if forwarded.Load() != 0 || guard.denied.Load() != 8 {
		t.Fatal("unsafe request reached the transport")
	}
}

func TestLiveReadonlyPaymentMethod(t *testing.T) { //nolint:gocyclo // Fail closed on discovery, identity, result, empty response and request boundaries.
	creds, u, encoded := readonlyQACredentials(t)
	old := http.DefaultTransport
	guard := &regionReadGuard{origin: u.Host, path: "/v1/payment-methods", next: old}
	http.DefaultTransport = guard
	defer func() { http.DefaultTransport = old }()

	api, err := newAPI(clients.Credentials{APIKey: creds[readonlyAPIKeyField], SecretKey: creds[readonlySecretKeyField], URL: creds[readonlyURLField]})
	if err != nil {
		t.Fatal("payment preflight client failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	methods, err := api.Account.ListPaymentMethods(ctx)
	cancel()
	if err != nil {
		t.Fatal("payment preflight read failed")
	}
	var cardType, cardSuffix, expectedID string
	for _, item := range methods {
		if item == nil || redis.IntValue(item.ID) <= 0 || redis.StringValue(item.Type) == "" {
			continue
		}
		candidateType, candidateSuffix := redis.StringValue(item.Type), selector.LastFour(item)
		selected, matchErr := selector.MatchPaymentMethod(methods, selector.PaymentMethodQuery{CardType: candidateType, LastFourNumbers: candidateSuffix, ExcludeExpired: true, Now: time.Now()})
		if matchErr == nil && redis.IntValue(selected.ID) == redis.IntValue(item.ID) {
			cardType, cardSuffix, expectedID = candidateType, candidateSuffix, strconv.Itoa(redis.IntValue(item.ID))
			break
		}
	}
	if expectedID == "" {
		t.Fatal("BLOCKED: no unique unexpired payment method is available")
	}

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1beta1.SchemeBuilder.AddToScheme, lookupv1alpha1.AddToScheme} {
		if add(scheme) != nil {
			t.Fatal("unable to initialize in-memory schema")
		}
	}
	const secretDataKey = "qa-key"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "payment-secret", Namespace: "payment-qa"}, Data: map[string][]byte{secretDataKey: encoded}}
	config := &v1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: exampleProviderConfig}, Spec: v1beta1.ProviderConfigSpec{Credentials: v1beta1.ProviderCredentials{Source: xpv1.CredentialsSourceSecret, CommonCredentialSelectors: xpv1.CommonCredentialSelectors{SecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Name: "payment-secret", Namespace: "payment-qa"}, Key: secretDataKey}}}}}
	payment := &lookupv1alpha1.PaymentMethod{ObjectMeta: metav1.ObjectMeta{Name: "qa-payment"}, Spec: lookupv1alpha1.PaymentMethodSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.PaymentMethodParameters{CardType: &cardType, LastFourNumbers: &cardSuffix}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(payment).WithObjects(secret, config, payment).Build()
	reconciler := &PaymentMethodReconciler{Client: kube}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	result, err := reconciler.Reconcile(ctx, request("qa-payment"))
	cancel()
	if err != nil || result.RequeueAfter != requeue {
		t.Fatal("payment reconciliation failed")
	}
	got := &lookupv1alpha1.PaymentMethod{}
	if kube.Get(context.Background(), types.NamespacedName{Name: "qa-payment"}, got) != nil || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) || got.Status.AtProvider.ID != expectedID || got.Status.AtProvider.CardType != cardType || got.Status.AtProvider.LastFourNumbers != cardSuffix {
		t.Fatal("payment lookup returned an unexpected result")
	}

	missingType := "__qualification_no_match__"
	got.Spec.ForProvider.CardType = &missingType
	got.Spec.ForProvider.LastFourNumbers = nil
	if kube.Update(context.Background(), got) != nil {
		t.Fatal("cannot update fake lookup filter")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	result, err = reconciler.Reconcile(ctx, request("qa-payment"))
	cancel()
	if err != nil || result.RequeueAfter != requeueFailed {
		t.Fatal("no-match lookup did not fail closed")
	}
	if kube.Get(context.Background(), types.NamespacedName{Name: "qa-payment"}, got) != nil || got.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionFalse || got.Status.AtProvider.ID != "" {
		t.Fatal("no-match lookup retained a stale result")
	}
	if guard.allowed.Load() != 3 || guard.ok.Load() != 3 || guard.denied.Load() != 0 {
		t.Fatal("unexpected payment request trace or HTTP result")
	}
	t.Log("Payment lookup: unique unexpired method Ready; missing filter clears status; 3 guarded GETs returned 200")
}
