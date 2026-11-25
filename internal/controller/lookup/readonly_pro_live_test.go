// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

func rejectDuplicateKeys(raw []byte) error { //nolint:gocyclo // Walk objects and arrays while rejecting duplicate keys and trailing data.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var readValue func() error
	readValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, isDelim := token.(json.Delim)
		if !isDelim {
			return nil
		}
		if delim == '{' {
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return errors.New("duplicate or malformed key")
				}
				seen[name] = true
				if err := readValue(); err != nil {
					return err
				}
			}
		} else if delim == '[' {
			for decoder.More() {
				if err := readValue(); err != nil {
					return err
				}
			}
		} else {
			return errors.New("unexpected delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := readValue(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

type proInventoryRow struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	DeploymentType string `json:"deploymentType"`
}

func uniqueProSelector(raw []byte) (int, error) { //nolint:gocyclo // Validate each completeness and ambiguity boundary independently.
	var body map[string]json.RawMessage
	if rejectDuplicateKeys(raw) != nil || json.Unmarshal(raw, &body) != nil || body == nil {
		return 0, errors.New("inventory malformed")
	}
	for _, k := range []string{"next", "nextPage", "nextToken", "cursor", "offset", "limit", "page", "pageSize", "hasMore", "hasNext", "pagination", "links", "_links", "truncated"} {
		if _, exists := body[k]; exists {
			return 0, errors.New("inventory paging not established")
		}
	}
	var account int
	if json.Unmarshal(body["accountId"], &account) != nil || account <= 0 {
		return 0, errors.New("account identity unavailable")
	}
	var rows []proInventoryRow
	if json.Unmarshal(body["subscriptions"], &rows) != nil || rows == nil || len(rows) >= 10000 {
		return 0, errors.New("inventory malformed or too large")
	}
	var total int
	if json.Unmarshal(body["total"], &total) != nil || total != len(rows) {
		return 0, errors.New("inventory completeness unverified")
	}
	seen := map[int]bool{}
	names := map[string]bool{}
	selected := 0
	count := 0
	for _, row := range rows {
		if row.ID <= 0 || strings.TrimSpace(row.Name) == "" || row.Status == "" || (row.DeploymentType != "single-region" && row.DeploymentType != "active-active") || seen[row.ID] || names[row.Name] {
			return 0, errors.New("inventory identity ambiguous")
		}
		seen[row.ID], names[row.Name] = true, true
		if row.Status == "active" && row.DeploymentType == "single-region" {
			selected, count = row.ID, count+1
		}
	}
	if count != 1 {
		return 0, errors.New("no unique active Pro subscription")
	}
	return selected, nil
}

func TestReadonlyProDiscoveryRejectsAmbiguity(t *testing.T) {
	valid := `{"accountId":1,"total":1,"subscriptions":[{"id":3,"name":"fixture","status":"active","deploymentType":"single-region"}]}`
	if id, err := uniqueProSelector([]byte(valid)); err != nil || id != 3 {
		t.Fatal("valid fixture was not accepted")
	}
	for _, raw := range []string{
		`{"accountId":1,"subscriptions":[{"id":3,"name":"fixture","status":"active","deploymentType":"single-region"}]}`,
		`{"accountId":1,"total":1,"next":"more","subscriptions":[]}`,
		`{"accountId":1,"total":0,"subscriptions":[]}`,
		`{"accountId":1,"total":2,"subscriptions":[{"id":3,"name":"a","status":"active","deploymentType":"single-region"},{"id":4,"name":"b","status":"active","deploymentType":"single-region"}]}`,
		`{"accountId":1,"total":2,"subscriptions":[{"id":3,"name":"a"},{"id":3,"name":"b"}]}`,
		`{"accountId":1,"total":2,"total":1,"subscriptions":[{"id":3,"name":"fixture","status":"active","deploymentType":"single-region"}]}`,
	} {
		if _, err := uniqueProSelector([]byte(raw)); err == nil {
			t.Fatal("unsafe inventory accepted")
		}
	}
}

func TestReadonlyProTransportRejectsUnsafeRequests(t *testing.T) {
	var forwarded atomic.Int32
	guard := &regionReadGuard{origin: readonlyFixtureHost, path: "/v1/subscriptions", next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		forwarded.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, item := range []struct{ method, path string }{{http.MethodPost, "/v1/subscriptions"}, {http.MethodDelete, "/v1/subscriptions"}, {http.MethodGet, "/v1/subscriptions/3"}, {http.MethodGet, "/v1/acl/users"}} {
		req, _ := http.NewRequest(item.method, "https://"+readonlyFixtureHost+item.path, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("unexpected request accepted")
		}
	}
	if forwarded.Load() != 0 || guard.denied.Load() != 4 {
		t.Fatal("unexpected request reached network")
	}
	guard.path = "/v1/subscriptions/3"
	wrong, _ := http.NewRequest(http.MethodGet, "https://"+readonlyFixtureHost+"/v1/subscriptions/4", nil)
	if _, err := guard.RoundTrip(wrong); err == nil || forwarded.Load() != 0 {
		t.Fatal("different subscription identity accepted")
	}
	exact, _ := http.NewRequest(http.MethodGet, "https://"+readonlyFixtureHost+"/v1/subscriptions/3", nil)
	response, err := guard.RoundTrip(exact)
	if err != nil || response.StatusCode != 200 || forwarded.Load() != 1 {
		t.Fatal("exact subscription identity rejected")
	}
	_ = response.Body.Close()
}

func TestLiveReadonlyProSubscription(t *testing.T) { //nolint:gocyclo // Fail closed at each account, inventory, identity and request boundary.
	const objectName = "qa-pro-lookup"
	creds, u, encoded := readonlyQACredentials(t)
	old := http.DefaultTransport
	guard := &regionReadGuard{origin: u.Host, path: "/v1/subscriptions", next: old}
	http.DefaultTransport = guard
	defer func() { http.DefaultTransport = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(creds[readonlyURLField], "/")+"/subscriptions", nil)
	if err != nil {
		cancel()
		t.Fatal("inventory request invalid")
	}
	req.Header.Set("X-Api-Key", creds[readonlyAPIKeyField])
	req.Header.Set("X-Api-Secret-Key", creds[readonlySecretKeyField])
	response, err := (&http.Client{Transport: guard}).Do(req)
	if err != nil {
		cancel()
		t.Fatal("authenticated inventory read failed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 10*1024*1024+1))
	_ = response.Body.Close()
	cancel()
	if readErr != nil || response.StatusCode != http.StatusOK || len(raw) > 10*1024*1024 || response.Header.Get("Link") != "" || response.Header.Get("X-Next-Page") != "" {
		t.Fatal("inventory response invalid or incomplete")
	}
	id, err := uniqueProSelector(raw)
	if err != nil {
		t.Fatal("BLOCKED: unique account-bound Pro selector or complete inventory not established")
	}
	guard.path = fmt.Sprintf("/v1/subscriptions/%d", id)
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1beta1.SchemeBuilder.AddToScheme, lookupv1alpha1.AddToScheme} {
		if add(scheme) != nil {
			t.Fatal("unable to initialize fake schema")
		}
	}
	const secretDataKey = "pro-key"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pro-secret", Namespace: "pro-qa"}, Data: map[string][]byte{secretDataKey: encoded}}
	config := &v1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: exampleProviderConfig}, Spec: v1beta1.ProviderConfigSpec{Credentials: v1beta1.ProviderCredentials{Source: xpv1.CredentialsSourceSecret, CommonCredentialSelectors: xpv1.CommonCredentialSelectors{SecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Name: "pro-secret", Namespace: "pro-qa"}, Key: secretDataKey}}}}}
	obj := &lookupv1alpha1.ProSubscription{ObjectMeta: metav1.ObjectMeta{Name: objectName}, Spec: lookupv1alpha1.ProSubscriptionSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: strconv.Itoa(id)}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(obj).WithObjects(secret, config, obj).Build()
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	result, err := (&ProSubscriptionReconciler{Client: kube}).Reconcile(ctx, request(objectName))
	cancel()
	if err != nil || result.RequeueAfter != requeue {
		t.Fatal("Pro lookup reconciliation failed")
	}
	got := &lookupv1alpha1.ProSubscription{}
	if kube.Get(context.Background(), types.NamespacedName{Name: objectName}, got) != nil || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) || got.Status.AtProvider.ID != strconv.Itoa(id) || got.Status.AtProvider.DeploymentType != "single-region" || got.Status.AtProvider.Status != "active" {
		t.Fatal("Pro lookup status or identity mismatch")
	}
	if guard.allowed.Load() != 2 || guard.ok.Load() != 2 || guard.denied.Load() != 0 {
		t.Fatal("unexpected Pro request trace")
	}
	t.Log("Pro lookup: unique complete account inventory and exact-ID reconciler GET passed; two guarded GETs returned 200")
}
