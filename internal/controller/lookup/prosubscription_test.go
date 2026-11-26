// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	rediscloud "github.com/RedisLabs/rediscloud-go-api"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type proFixtureTransport func(*http.Request) (*http.Response, error)

func (f proFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const proFixtureName = "subject"

// The HTTPS URLs below use a synthetic transport, not a TLS connection.
func TestProSubscriptionHTTPSRedirectsAndTransportError(t *testing.T) { //nolint:gocyclo // Validate each redirect and transport-error boundary.
	for _, tt := range []struct {
		name, location string
		code           int
	}{
		{"same-origin", "https://fixture.invalid/other", http.StatusFound},
		{"cross-origin", "https://other.invalid/other", http.StatusTemporaryRedirect},
		{"permanent", "https://other.invalid/other", http.StatusPermanentRedirect},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			transport := proFixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "fixture.invalid" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
					t.Error("unexpected target or missing fixture credentials")
				}
				return &http.Response{StatusCode: tt.code, Header: http.Header{"Location": []string{tt.location}}, Body: io.NopCloser(strings.NewReader("fixture-sensitive-body")), Request: r}, nil
			})
			api, err := rediscloud.NewClient(rediscloud.Auth(exampleAPIKey, exampleAPISecret), rediscloud.BaseURL("https://fixture.invalid"), rediscloud.Transporter(noRedirectTransport{next: transport}))
			if err != nil {
				t.Fatal(err)
			}
			obj := &lookupv1alpha1.ProSubscription{Spec: lookupv1alpha1.ProSubscriptionSpec{ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}}
			_, err = readProSubscription(context.Background(), api, obj)
			if err == nil || err.Error() != "cannot get Pro subscription from Redis Cloud" || calls != 1 {
				t.Fatalf("redirect was not safely rejected: calls=%d err=%v", calls, err)
			}
		})
	}
	t.Run("transport-error", func(t *testing.T) {
		transport := proFixtureTransport(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("fixture-sensitive-transport-error")
		})
		api, err := rediscloud.NewClient(rediscloud.Auth(exampleAPIKey, exampleAPISecret), rediscloud.BaseURL("https://fixture.invalid"), rediscloud.Transporter(noRedirectTransport{next: transport}))
		if err != nil {
			t.Fatal(err)
		}
		obj := &lookupv1alpha1.ProSubscription{Spec: lookupv1alpha1.ProSubscriptionSpec{ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}}
		_, err = readProSubscription(context.Background(), api, obj)
		if err == nil || err.Error() != "cannot get Pro subscription from Redis Cloud" {
			t.Fatal("transport error was not sanitized")
		}
	})
}

func TestProSubscriptionLookupReadOnlyAndFailure(t *testing.T) { //nolint:gocyclo // Exercise the same controller over success and invalid responses.
	for _, tt := range []struct {
		name, id, body string
		code           int
		ready          bool
		wantCalls      int
	}{
		{"pro", "123", `{"id":123,"name":"fixture","status":"active","deploymentType":"single-region"}`, 200, true, 1},
		{"forbidden", "123", `{}`, 403, false, 1},
		{"not-found", "123", `{}`, 404, false, 1},
		{"server-error", "123", `{}`, 500, false, 1},
		{"empty-response", "123", `{}`, 200, false, 1},
		{"wrong-id", "123", `{"id":124,"deploymentType":"single-region"}`, 200, false, 1},
		{"active-active", "123", `{"id":123,"deploymentType":"active-active"}`, 200, false, 1},
		{"unknown-type", "123", `{"id":123}`, 200, false, 1},
		{"invalid-id", "-1", `{}`, 200, false, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/subscriptions/123" || r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
					t.Errorf("unexpected or unauthenticated request %s %s", r.Method, r.URL.Path)
					http.Error(w, "denied", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(tt.body))
			})
			obj := &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: tt.id}}, Status: lookupv1alpha1.ProSubscriptionStatus{AtProvider: lookupv1alpha1.ProSubscriptionObservation{ID: "stale"}}}
			kube := lookupClient(t, h, obj)
			rec := &ProSubscriptionReconciler{Client: kube}
			result, err := rec.Reconcile(context.Background(), request(proFixtureName))
			if err != nil {
				t.Fatal(err)
			}
			got := &lookupv1alpha1.ProSubscription{}
			if err = kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, got); err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, tt.wantCalls)
			}
			if tt.ready {
				if result.RequeueAfter != requeue || got.Status.AtProvider.ID != "123" || got.Status.AtProvider.Name != "fixture" || got.Status.AtProvider.Status != "active" || got.Status.AtProvider.DeploymentType != "single-region" {
					t.Fatalf("unexpected ready status=%+v result=%+v", got.Status.AtProvider, result)
				}
			} else if result.RequeueAfter != requeueFailed || got.Status.AtProvider.ID != "" {
				t.Fatalf("failure did not clear stale status: %+v", got.Status.AtProvider)
			}
		})
	}
}

func TestProSubscriptionRejectsRedirectWithoutLeakingCredentials(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
	}))
	t.Cleanup(target.Close)
	obj := &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}, Status: lookupv1alpha1.ProSubscriptionStatus{AtProvider: lookupv1alpha1.ProSubscriptionObservation{ID: "stale"}}}
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != exampleAPIKey || r.Header.Get("X-Api-Secret-Key") != exampleAPISecret {
			t.Error("origin did not receive fixture credentials")
		}
		http.Redirect(w, r, target.URL+"/unexpected", http.StatusFound)
	}), obj)
	result, err := (&ProSubscriptionReconciler{Client: kube}).Reconcile(context.Background(), request(proFixtureName))
	if err != nil || result.RequeueAfter != requeueFailed || targetCalls.Load() != 0 {
		t.Fatalf("redirect was followed or failed to requeue: calls=%d result=%+v err=%v", targetCalls.Load(), result, err)
	}
	got := &lookupv1alpha1.ProSubscription{}
	if err = kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, got); err != nil || got.Status.AtProvider.ID != "" || got.Status.GetCondition(xpv1.TypeReady).Status != "False" {
		t.Fatal("redirect did not clear status and mark lookup unavailable")
	}
}

func TestProSubscriptionDoesNotExposeAPIErrorBody(t *testing.T) {
	const sentinel = "fixture-sensitive-error-body"
	obj := &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}, Status: lookupv1alpha1.ProSubscriptionStatus{AtProvider: lookupv1alpha1.ProSubscriptionObservation{ID: "stale"}}}
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, sentinel, http.StatusForbidden)
	}), obj)
	result, err := (&ProSubscriptionReconciler{Client: kube}).Reconcile(context.Background(), request(proFixtureName))
	if err != nil || result.RequeueAfter != requeueFailed {
		t.Fatal("failed API read did not requeue")
	}
	got := &lookupv1alpha1.ProSubscription{}
	if err = kube.Get(context.Background(), types.NamespacedName{Name: proFixtureName}, got); err != nil {
		t.Fatal(err)
	}
	for _, condition := range got.Status.Conditions {
		if strings.Contains(condition.Message, sentinel) {
			t.Fatal("API response body leaked into status")
		}
	}
	if got.Status.AtProvider.ID != "" || got.Status.GetCondition(xpv1.TypeReady).Status != "False" {
		t.Fatal("API failure did not clear status and mark lookup unavailable")
	}
}

func TestProSubscriptionDeleteDoesNotReadCloud(t *testing.T) {
	now := metav1.NewTime(time.Now())
	obj := &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: proFixtureName, DeletionTimestamp: &now, Finalizers: []string{"test"}}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}}
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected Cloud call %s", r.Method) }), obj)
	result, err := (&ProSubscriptionReconciler{Client: kube}).Reconcile(context.Background(), request(proFixtureName))
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("delete result=%+v err=%v", result, err)
	}
}
