// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/apis/v1beta1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// regionReadGuard denies writes, redirects to any other path or host, queries,
// and plaintext before the request reaches the real network transport.
type regionReadGuard struct {
	origin              string
	path                string
	next                http.RoundTripper
	allowed, denied, ok atomic.Int32
}

func (g *regionReadGuard) RoundTrip(r *http.Request) (*http.Response, error) { //nolint:gocyclo // Each condition is a distinct fail-closed request boundary.
	if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != g.origin || r.URL.Path != g.path || r.URL.RawQuery != "" || r.URL.User != nil || r.Body != nil {
		g.denied.Add(1)
		return nil, errors.New("request denied by read-only region guard")
	}
	g.allowed.Add(1)
	resp, err := g.next.RoundTrip(r)
	if err == nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		_ = resp.Body.Close()
		g.denied.Add(1)
		return nil, errors.New("redirect denied by read-only region guard")
	}
	if err == nil && resp.StatusCode == http.StatusOK {
		g.ok.Add(1)
	}
	return resp, err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReadonlyRegionTransportRejectsUnsafeRequests(t *testing.T) { //nolint:gocyclo // Exercise every rejected request and redirect in one isolated guard test.
	const fixtureURL = "https://qa.example.test/v1/regions"
	var forwarded atomic.Int32
	guard := &regionReadGuard{origin: "qa.example.test", path: "/v1/regions", next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		forwarded.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, item := range []struct{ method, raw string }{
		{"POST", fixtureURL}, {"DELETE", fixtureURL},
		{"PUT", fixtureURL}, {"PATCH", fixtureURL},
		{"GET", "https://elsewhere.example.test/v1/regions"}, {"GET", "http://qa.example.test/v1/regions"},
		{"GET", "https://qa.example.test/v1/subscriptions"}, {"GET", "https://qa.example.test/v1/regions?x=1"},
		{"GET", "https://qa.example.test/v1/payment-methods"},
	} {
		req, err := http.NewRequest(item.method, item.raw, nil)
		if err != nil {
			t.Fatal("invalid fixture request")
		}
		if _, err = guard.RoundTrip(req); err == nil {
			t.Fatal("unsafe fixture request was accepted")
		}
	}
	if forwarded.Load() != 0 || guard.denied.Load() != 9 {
		t.Fatal("unsafe fixture request reached transport")
	}
	withBody, _ := http.NewRequest("GET", fixtureURL, strings.NewReader("data"))
	if _, err := guard.RoundTrip(withBody); err == nil || forwarded.Load() != 0 {
		t.Fatal("GET request with body reached transport")
	}
	req, _ := http.NewRequest("GET", fixtureURL, nil)
	response, err := guard.RoundTrip(req)
	if err != nil || response.StatusCode != http.StatusOK || forwarded.Load() != 1 {
		t.Fatal("allowed fixture request failed")
	}
	_ = response.Body.Close()
	guard.next = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{fixtureURL}}}, nil
	})
	if _, err = guard.RoundTrip(req); err == nil {
		t.Fatal("redirect was accepted")
	}
}

func readonlyQACredentials(t *testing.T) (map[string]string, *url.URL, []byte) { //nolint:gocyclo // Reject each invalid hosted credential and origin boundary.
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("GITHUB_REPOSITORY") != "RedisLabs/crossplane-provider-rediscloud" || os.Getenv("QA_READONLY_REGIONS") != "1" {
		t.Skip("hosted opt-in required")
	}
	var creds map[string]string
	raw := os.Getenv("UPTEST_CLOUD_CREDENTIALS")
	if json.Unmarshal([]byte(raw), &creds) != nil || creds["api_key"] == "" || creds["secret_key"] == "" {
		t.Fatal("QA credentials unavailable")
	}
	u, err := url.Parse(creds["url"])
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.TrimRight(u.Path, "/") != "/v1" || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("QA endpoint invalid")
	}
	sum := sha256.Sum256([]byte(u.Scheme + "://" + u.Host))
	expected := hex.EncodeToString(sum[:])
	allowed := false
	for _, item := range strings.Split(os.Getenv("QA_VERIFIED_ORIGIN_HASHES"), ",") {
		item = strings.TrimSpace(item)
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(item) {
			t.Fatal("QA origin allowlist invalid")
		}
		allowed = allowed || item == expected
	}
	if !allowed {
		t.Fatal("QA origin is not independently allowlisted")
	}
	creds["url"] = strings.TrimRight(creds["url"], "/")
	encoded, err := json.Marshal(creds)
	if err != nil {
		t.Fatal("QA credentials could not be normalized")
	}
	return creds, u, encoded
}

// TestLiveReadonlyRegions is invoked only by the explicitly dispatched hosted
// workflow. It makes only guarded Cloud reads and uses a fake Kubernetes client.
func TestLiveReadonlyRegions(t *testing.T) { //nolint:gocyclo // Fail closed at every request and result boundary.
	const secretDataKey = "qa-key"
	_, u, encoded := readonlyQACredentials(t)

	old := http.DefaultTransport
	guard := &regionReadGuard{origin: u.Host, path: "/v1/regions", next: old}
	http.DefaultTransport = guard
	defer func() { http.DefaultTransport = old }()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1beta1.SchemeBuilder.AddToScheme, lookupv1alpha1.AddToScheme} {
		if add(scheme) != nil {
			t.Fatal("unable to initialize in-memory schema")
		}
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "readonly-secret", Namespace: "qa-readonly"}, Data: map[string][]byte{secretDataKey: encoded}}
	config := &v1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: "default"}, Spec: v1beta1.ProviderConfigSpec{Credentials: v1beta1.ProviderCredentials{Source: xpv1.CredentialsSourceSecret, CommonCredentialSelectors: xpv1.CommonCredentialSelectors{SecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Name: "readonly-secret", Namespace: "qa-readonly"}, Key: secretDataKey}}}}}
	for _, provider := range []string{"AWS", "GCP"} {
		region := &lookupv1alpha1.Regions{ObjectMeta: metav1.ObjectMeta{Name: "qa-regions"}, Spec: lookupv1alpha1.RegionsSpec{ProviderConfigRef: xpv1.Reference{Name: "default"}, ForProvider: lookupv1alpha1.RegionsParameters{ProviderName: &provider}}}
		kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(region).WithObjects(secret.DeepCopy(), config.DeepCopy(), region).Build()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result, err := (&RegionsReconciler{Client: kube}).Reconcile(ctx, request("qa-regions"))
		cancel()
		if err != nil || result.RequeueAfter != requeue {
			t.Fatal("region reconciliation failed")
		}
		got := &lookupv1alpha1.Regions{}
		if kube.Get(context.Background(), types.NamespacedName{Name: "qa-regions"}, got) != nil || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) || len(got.Status.AtProvider.Regions) == 0 {
			t.Fatal("region lookup was not Ready or returned no results")
		}
		for _, entry := range got.Status.AtProvider.Regions {
			if entry.ProviderName != provider || entry.RegionID == "" || entry.Name == "" {
				t.Fatal("region lookup contained an invalid entry")
			}
		}
		t.Logf("%s regions: authenticated lookup Ready; matching result count=%d", provider, len(got.Status.AtProvider.Regions))
	}
	if guard.allowed.Load() != 2 || guard.ok.Load() != 2 || guard.denied.Load() != 0 {
		t.Fatal("unexpected request trace or Cloud response")
	}
	t.Log("Cloud request trace: 2 guarded GET /v1/regions; HTTP 200; no other HTTP requests")
}
