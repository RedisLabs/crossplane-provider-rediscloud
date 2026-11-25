package qatfplan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acl "github.com/RedisLabs/provider-rediscloud/apis/acl/v1beta1"
	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	ujcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kexec "k8s.io/utils/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestACLRoleManagedLifecycleLoopback runs actual managed reconciliation and
// Terraform against a closed, credential-free loopback fixture, never Cloud.
const fixtureUncertainMode = "uncertain"
const fixtureACLRolePath = "/v1/acl/roles/42"
const fixtureACLRolesPath = "/v1/acl/roles"

func TestACLRoleManagedLifecycleLoopback(t *testing.T) {
	t.Run("create_rejected", func(t *testing.T) { runACLRoleFixture(t, "rejected") })
	t.Run("accepted_then_503", func(t *testing.T) { runACLRoleFixture(t, fixtureUncertainMode) })
	t.Run("lifecycle", func(t *testing.T) { runACLRoleFixture(t, "success") })
}

func runACLRoleFixture(t *testing.T, mode string) { //nolint:gocyclo // The fixture checks the complete ordered lifecycle.
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	var mu sync.Mutex
	exists, unexpected := false, false
	rejectCreate, rejectUpdate, rejectDelete := mode != "success", true, true
	createRequests := 0
	rule := "fixture-rule-a"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			unexpected = true
			http.Error(w, "fixture credentials required", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch {
		case r.Method == http.MethodPost && r.URL.Path == fixtureACLRolesPath && !exists:
			createRequests++
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body[fixtureNameKey] != "fixture-acl-role" || !validFakeRoleAssociation(body, rule) {
				unexpected = true
				http.Error(w, "bad create", http.StatusBadRequest)
				return
			}
			if rejectCreate {
				rejectCreate = false
				if mode == fixtureUncertainMode {
					exists = true
					http.Error(w, "injected accepted create with unavailable response", http.StatusServiceUnavailable)
					return
				}
				http.Error(w, "injected create rejection", http.StatusForbidden)
				return
			}
			exists = true
			response = map[string]any{fixtureTaskKey: "create"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/create":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureTaskResourceKey: 42}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureACLRolePath && exists:
			response = map[string]any{"id": 42, fixtureNameKey: "fixture-acl-role", "redisRules": []any{map[string]any{"ruleName": rule, "databases": []any{map[string]any{"subscriptionId": 1, fixtureDatabaseIDKey: 2, "regions": []any{}}}}}, fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodPut && r.URL.Path == fixtureACLRolePath && exists:
			if rejectUpdate {
				rejectUpdate = false
				http.Error(w, "injected update rejection", http.StatusForbidden)
				return
			}
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || !validFakeRoleAssociation(body, fixtureNextRole) {
				unexpected = true
				http.Error(w, "bad update", http.StatusBadRequest)
				return
			}
			rule = fixtureNextRole
			response = map[string]any{fixtureTaskKey: "update"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/update":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureACLRolePath && exists:
			if rejectDelete {
				rejectDelete = false
				http.Error(w, "injected delete rejection", http.StatusForbidden)
				return
			}
			exists = false
			response = map[string]any{fixtureTaskKey: "delete"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/delete":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == fixtureACLRolePath && !exists:
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == fixtureACLRolesPath:
			rows := []any{map[string]any{"id": 99, fixtureNameKey: "foreign-role", fixtureStatusKey: fixtureActive}}
			if exists {
				rows = append(rows, map[string]any{"id": 42, fixtureNameKey: "fixture-acl-role", "redisRules": []any{map[string]any{"ruleName": rule, "databases": []any{map[string]any{"subscriptionId": 1, fixtureDatabaseIDKey: 2, "regions": []any{}}}}}, fixtureStatusKey: fixtureActive})
			}
			response = map[string]any{"roles": rows}
		default:
			unexpected = true
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("TMPDIR", dir)
	t.Setenv("TF_CLI_CONFIG_FILE", filepath.Join(dir, "offline.tfrc"))
	for _, key := range []string{"TF_CLI_ARGS", "TF_CLI_ARGS_init", "TF_CLI_ARGS_apply", "TF_CLI_ARGS_plan", "TF_CLI_ARGS_destroy"} {
		t.Setenv(key, "")
	}
	for _, key := range []string{fixtureHTTPProxy, fixtureHTTPSProxy, fixtureAllProxy, fixtureHTTPProxyLower, fixtureHTTPSProxyLower, fixtureAllProxyLower} {
		t.Setenv(key, "http://127.0.0.1:1")
	}
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	t.Setenv("REDISCLOUD_URL", server.URL+"/v1")
	t.Setenv("REDISCLOUD_ACCESS_KEY", dummyCredential)
	t.Setenv("REDISCLOUD_SECRET_KEY", dummyCredential)
	t.Setenv("PATH", filepath.Dir(tf)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "offline.tfrc"), []byte(fmt.Sprintf("provider_installation {\n filesystem_mirror { path = %q }\n}\n", plugins)), 0600); err != nil {
		t.Fatal(err)
	}
	scheme := runtime.NewScheme()
	if err := acl.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	name := fixtureName
	value := rule
	subID, dbID := float64(1), float64(2)
	resourceName := "fixture-acl-role"
	item := &acl.Role{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("fixture-acl-role-uid")}}
	item.Spec.ForProvider.Name = &resourceName
	item.Spec.ForProvider.Rule = []acl.RuleParameters{{Name: &value, Database: []acl.DatabaseParameters{{Subscription: &subID, Database: &dbID}}}}
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	store := newTrackingNativeStore()
	spy := &spyExecutor{Interface: kexec.New(), tracked: store}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_acl_role"], ujcontroller.WithCallbackProvider(callback))
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(acl.Role_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
	await := func(command string, expectFailure bool) {
		t.Helper()
		for i := 0; i < 8; i++ {
			if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-callback.done:
				if (err != nil) != expectFailure {
					t.Fatalf("unexpected operation result: %v", err)
				}
				return
			case <-time.After(2 * time.Second):
			}
			for _, actual := range spy.snapshot() {
				if strings.Contains(actual, command) && !strings.Contains(actual, "-refresh-only") {
					select {
					case err := <-callback.done:
						if (err != nil) != expectFailure {
							t.Fatalf("unexpected operation result: %v", err)
						}
						return
					case <-time.After(90 * time.Second):
						t.Fatal("callback timeout")
					}
				}
			}
		}
		t.Fatal("callback missing")
	}
	if mode != "success" {
		await("terraform apply ", true)
		for i := 0; i < 3; i++ {
			if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
				t.Fatal(err)
			}
		}
		mu.Lock()
		if exists != (mode == fixtureUncertainMode) || unexpected || createRequests != 1 {
			mu.Unlock()
			t.Fatal("failed create was replayed or fake outcome changed")
		}
		mu.Unlock()
		verifyForeignACLInventory(t, server.URL+fixtureACLRolesPath, "roles", "foreign-role", "fixture-acl-role", mode == fixtureUncertainMode)
		return
	}
	await("terraform apply ", false)
	for i := 0; i < 5; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		current := &acl.Role{}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
			t.Fatal(err)
		}
		if current.Status.AtProvider.ID != nil && *current.Status.AtProvider.ID == "42" {
			item = current
			break
		}
	}
	if item.Status.AtProvider.ID == nil || *item.Status.AtProvider.ID != "42" {
		t.Fatal("ACL identity missing")
	}
	updated := fixtureNextRole
	item.Spec.ForProvider.Rule[0].Name = &updated
	if err := kube.Update(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	verifyManagedDrift(t, rec, spy, callback, true)
	mu.Lock()
	if !exists || rule != "fixture-rule-a" || unexpected {
		mu.Unlock()
		t.Fatal("rejected update changed fake resource")
	}
	mu.Unlock()
	verifyManagedDrift(t, rec, spy, callback, false)
	mu.Lock()
	if !exists || rule != updated || unexpected {
		mu.Unlock()
		t.Fatal("ACL update or fixture trace invalid")
	}
	mu.Unlock()
	current := &acl.Role{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.AtProvider.ID == nil || *current.Status.AtProvider.ID != "42" {
		t.Fatal("ACL ID changed")
	}
	if err := kube.Delete(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	await("terraform destroy ", true)
	mu.Lock()
	if !exists || unexpected {
		mu.Unlock()
		t.Fatal("rejected delete changed fake resource")
	}
	mu.Unlock()
	await("terraform destroy ", false)
	for i := 0; i < 30; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		if apierrors.IsNotFound(kube.Get(context.Background(), types.NamespacedName{Name: name}, &acl.Role{})) {
			break
		}
		if i == 29 {
			item := &acl.Role{}
			_ = kube.Get(context.Background(), types.NamespacedName{Name: name}, item)
			mu.Lock()
			t.Logf("fixture exists=%v unexpected=%v", exists, unexpected)
			mu.Unlock()
			t.Fatalf("ACL CR remains: status=%#v finalizers=%v annotations=%v commands=%v", item.Status, item.Finalizers, item.Annotations, spy.snapshot())
		}
		time.Sleep(time.Second)
	}
	mu.Lock()
	if exists || unexpected {
		mu.Unlock()
		t.Fatal("fake ACL residue or unexpected request")
	}
	mu.Unlock()
	for path, status := range map[string]int{fixtureACLRolePath: http.StatusNotFound, fixtureACLRolesPath: http.StatusOK} {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Api-Key", dummyCredential)
		req.Header.Set("X-Api-Secret-Key", dummyCredential)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			_ = res.Body.Close()
			t.Fatalf("fake read %s: %d", path, res.StatusCode)
		}
		if status == http.StatusOK {
			var list struct {
				Rules []struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"roles"`
			}
			err = json.NewDecoder(res.Body).Decode(&list)
			if err != nil || len(list.Rules) != 1 || list.Rules[0].ID != 99 || list.Rules[0].Name != "foreign-role" {
				_ = res.Body.Close()
				t.Fatal("fake ACL inventory incomplete or nonempty")
			}
		}
		_ = res.Body.Close()
	}
	if _, err := os.Stat(filepath.Join(dir, "fixture-acl-role-uid")); !os.IsNotExist(err) {
		t.Fatal("workspace remains")
	}
}

func validFakeRoleAssociation(body map[string]any, expected string) bool {
	rules, ok := body["redisRules"].([]any)
	if !ok || len(rules) != 1 {
		return false
	}
	entry, ok := rules[0].(map[string]any)
	if !ok || entry["ruleName"] != expected {
		return false
	}
	databases, ok := entry["databases"].([]any)
	if !ok || len(databases) != 1 {
		return false
	}
	database, ok := databases[0].(map[string]any)
	return ok && database["subscriptionId"] == float64(1) && database[fixtureDatabaseIDKey] == float64(2)
}

func verifyForeignACLInventory(t *testing.T, url, key, foreignName, ownedName string, expectOwned bool) { //nolint:gocyclo // Reject incomplete or changed fake inventory.
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", dummyCredential)
	req.Header.Set("X-Api-Secret-Key", dummyCredential)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string][]map[string]any
	err = json.NewDecoder(res.Body).Decode(&body)
	_ = res.Body.Close()
	expectedCount := 1
	if expectOwned {
		expectedCount++
	}
	if err != nil || res.StatusCode != http.StatusOK || len(body[key]) != expectedCount || body[key][0]["id"] != float64(99) || body[key][0][fixtureNameKey] != foreignName {
		t.Fatal("foreign fake ACL inventory was not preserved")
	}
	if expectOwned && (body[key][1]["id"] != float64(42) || body[key][1][fixtureNameKey] != ownedName) {
		t.Fatal("uncertain fake create identity was not preserved")
	}
}
