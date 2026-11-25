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
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/redis_rules"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReadonlyACLTransportRejectsUnsafeRequests(t *testing.T) { //nolint:gocyclo // Check each disallowed route and mutation method before forwarding.
	const aclURL = "https://qa.example.test/v1/acl/redisRules"
	var forwarded atomic.Int32
	guard := &regionReadGuard{origin: readonlyFixtureHost, path: "/v1/acl/redisRules", next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		forwarded.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, raw := range []string{"https://qa.example.test/v1/regions", "https://qa.example.test/v1/acl/roles", "https://other.example.test/v1/acl/redisRules", "https://qa.example.test/v1/acl/redisRules?limit=1"} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("unexpected route was accepted")
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(method, aclURL, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("mutation method was accepted")
		}
	}
	if forwarded.Load() != 0 || guard.denied.Load() != 8 {
		t.Fatal("unsafe request reached the transport")
	}
}

func TestLiveReadonlyACLRule(t *testing.T) { //nolint:gocyclo // Fail closed on discovery, identity, result, empty response and request boundaries.
	creds, u, encoded := readonlyQACredentials(t)
	old := http.DefaultTransport
	guard := &regionReadGuard{origin: u.Host, path: "/v1/acl/redisRules", next: old}
	http.DefaultTransport = guard
	defer func() { http.DefaultTransport = old }()
	api, err := newAPI(clients.Credentials{APIKey: creds[readonlyAPIKeyField], SecretKey: creds[readonlySecretKeyField], URL: creds[readonlyURLField]})
	if err != nil {
		t.Fatal("ACL preflight client failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	rules, err := api.RedisRules.List(ctx)
	cancel()
	if err != nil {
		t.Fatal("ACL preflight read failed")
	}
	var ruleName, expectedID, expectedRule string
	for _, item := range rules {
		if item == nil || redis.IntValue(item.ID) <= 0 || redis.StringValue(item.Name) == "" {
			continue
		}
		candidate := redis.StringValue(item.Name)
		selected, matchErr := selector.MatchNamed(rules, candidate, func(rule *redis_rules.GetRedisRuleResponse) string { return redis.StringValue(rule.Name) })
		if matchErr == nil && redis.IntValue(selected.ID) == redis.IntValue(item.ID) {
			ruleName, expectedID, expectedRule = candidate, strconv.Itoa(redis.IntValue(item.ID)), redis.StringValue(item.ACL)
			break
		}
	}
	if expectedID == "" {
		t.Fatal("BLOCKED: no uniquely selectable ACL rule is available")
	}
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1beta1.SchemeBuilder.AddToScheme, lookupv1alpha1.AddToScheme} {
		if add(scheme) != nil {
			t.Fatal("unable to initialize in-memory schema")
		}
	}
	const secretDataKey = "acl-key"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "acl-secret", Namespace: "acl-qa"}, Data: map[string][]byte{secretDataKey: encoded}}
	config := &v1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: exampleProviderConfig}, Spec: v1beta1.ProviderConfigSpec{Credentials: v1beta1.ProviderCredentials{Source: xpv1.CredentialsSourceSecret, CommonCredentialSelectors: xpv1.CommonCredentialSelectors{SecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Name: "acl-secret", Namespace: "acl-qa"}, Key: secretDataKey}}}}}
	obj := &lookupv1alpha1.ACLRule{ObjectMeta: metav1.ObjectMeta{Name: "qa-acl"}, Spec: lookupv1alpha1.ACLRuleSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.ACLRuleParameters{Name: ruleName}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(obj).WithObjects(secret, config, obj).Build()
	reconciler := &ACLRuleReconciler{Client: kube}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	result, err := reconciler.Reconcile(ctx, request("qa-acl"))
	cancel()
	if err != nil || result.RequeueAfter != requeue {
		t.Fatal("ACL reconciliation failed")
	}
	got := &lookupv1alpha1.ACLRule{}
	if kube.Get(context.Background(), types.NamespacedName{Name: "qa-acl"}, got) != nil || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) || got.Status.AtProvider.ID != expectedID || got.Status.AtProvider.Name != ruleName || got.Status.AtProvider.Rule != expectedRule {
		t.Fatal("ACL lookup returned an unexpected result")
	}
	got.Spec.ForProvider.Name = "__qualification_no_match__"
	if kube.Update(context.Background(), got) != nil {
		t.Fatal("cannot update fake lookup filter")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	result, err = reconciler.Reconcile(ctx, request("qa-acl"))
	cancel()
	if err != nil || result.RequeueAfter != requeueFailed {
		t.Fatal("no-match lookup did not fail closed")
	}
	if kube.Get(context.Background(), types.NamespacedName{Name: "qa-acl"}, got) != nil || got.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionFalse || got.Status.AtProvider.ID != "" || got.Status.AtProvider.Rule != "" {
		t.Fatal("no-match lookup retained a stale result")
	}
	if guard.allowed.Load() != 3 || guard.ok.Load() != 3 || guard.denied.Load() != 0 {
		t.Fatal("unexpected ACL request trace or HTTP result")
	}
	t.Log("ACL rule lookup: unique rule Ready; missing filter clears status; 3 guarded GETs returned 200")
}
