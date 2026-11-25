// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/apis/v1beta1"
	"github.com/RedisLabs/provider-rediscloud/internal/clients"
	selector "github.com/RedisLabs/provider-rediscloud/internal/lookup"
	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/roles"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReadonlyACLRoleTransportRejectsUnsafeRequests(t *testing.T) { //nolint:gocyclo // Check each disallowed route and mutation method before forwarding.
	const roleURL = "https://qa.example.test/v1/acl/roles"
	var forwarded atomic.Int32
	guard := &regionReadGuard{origin: "qa.example.test", path: "/v1/acl/roles", next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		forwarded.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, raw := range []string{"https://qa.example.test/v1/regions", "https://qa.example.test/v1/acl/redisRules", "https://other.example.test/v1/acl/roles", "https://qa.example.test/v1/acl/roles?limit=1"} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("unexpected route was accepted")
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(method, roleURL, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("mutation method was accepted")
		}
	}
	if forwarded.Load() != 0 || guard.denied.Load() != 8 {
		t.Fatal("unsafe request reached the transport")
	}
}

func TestLiveReadonlyACLRole(t *testing.T) { //nolint:gocyclo // Fail closed on discovery, identity, result, empty response and request boundaries.
	const objectName = "qa-acl-role"
	creds, u, encoded := readonlyQACredentials(t)
	old := http.DefaultTransport
	guard := &regionReadGuard{origin: u.Host, path: "/v1/acl/roles", next: old}
	http.DefaultTransport = guard
	defer func() { http.DefaultTransport = old }()
	api, err := newAPI(clients.Credentials{APIKey: creds["api_key"], SecretKey: creds["secret_key"], URL: creds["url"]})
	if err != nil {
		t.Fatal("ACL role preflight client failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	items, err := api.Roles.List(ctx)
	cancel()
	if err != nil {
		t.Fatal("ACL role preflight read failed")
	}
	var roleName string
	var expected lookupv1alpha1.ACLRoleObservation
	for _, item := range items {
		if item == nil || redis.IntValue(item.ID) <= 0 || redis.StringValue(item.Name) == "" {
			continue
		}
		candidate := redis.StringValue(item.Name)
		selected, matchErr := selector.MatchNamed(items, candidate, func(role *roles.GetRoleResponse) string { return redis.StringValue(role.Name) })
		if matchErr == nil && redis.IntValue(selected.ID) == redis.IntValue(item.ID) {
			roleName, expected = candidate, selector.ObserveACLRole(selected)
			break
		}
	}
	if roleName == "" {
		t.Fatal("BLOCKED: no uniquely selectable ACL role is available")
	}
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1beta1.SchemeBuilder.AddToScheme, lookupv1alpha1.AddToScheme} {
		if add(scheme) != nil {
			t.Fatal("unable to initialize in-memory schema")
		}
	}
	const secretDataKey = "role-key"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "role-secret", Namespace: "role-qa"}, Data: map[string][]byte{secretDataKey: encoded}}
	config := &v1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: exampleProviderConfig}, Spec: v1beta1.ProviderConfigSpec{Credentials: v1beta1.ProviderCredentials{Source: xpv1.CredentialsSourceSecret, CommonCredentialSelectors: xpv1.CommonCredentialSelectors{SecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Name: "role-secret", Namespace: "role-qa"}, Key: secretDataKey}}}}}
	obj := &lookupv1alpha1.ACLRole{ObjectMeta: metav1.ObjectMeta{Name: objectName}, Spec: lookupv1alpha1.ACLRoleSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.ACLRoleParameters{Name: roleName}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(obj).WithObjects(secret, config, obj).Build()
	reconciler := &ACLRoleReconciler{Client: kube}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	result, err := reconciler.Reconcile(ctx, request(objectName))
	cancel()
	if err != nil || result.RequeueAfter != requeue {
		t.Fatal("ACL role reconciliation failed")
	}
	got := &lookupv1alpha1.ACLRole{}
	if kube.Get(context.Background(), types.NamespacedName{Name: objectName}, got) != nil || !got.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) || !reflect.DeepEqual(got.Status.AtProvider, expected) {
		t.Fatal("ACL role lookup returned an unexpected result")
	}
	got.Spec.ForProvider.Name = "__qualification_no_match__"
	if kube.Update(context.Background(), got) != nil {
		t.Fatal("cannot update fake lookup filter")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	result, err = reconciler.Reconcile(ctx, request(objectName))
	cancel()
	if err != nil || result.RequeueAfter != requeueFailed {
		t.Fatal("no-match role lookup did not fail closed")
	}
	if kube.Get(context.Background(), types.NamespacedName{Name: objectName}, got) != nil || got.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionFalse || got.Status.AtProvider.ID != "" || len(got.Status.AtProvider.Rules) != 0 {
		t.Fatal("no-match role lookup retained a stale result")
	}
	if guard.allowed.Load() != 3 || guard.ok.Load() != 3 || guard.denied.Load() != 0 {
		t.Fatal("unexpected ACL role request trace or HTTP result")
	}
	t.Log("ACL role lookup: unique role Ready; missing filter clears status; 3 guarded GETs returned 200")
}
