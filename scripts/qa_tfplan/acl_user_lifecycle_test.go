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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kexec "k8s.io/utils/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestACLUserManagedLifecycleLoopback runs actual managed reconciliation and
// Terraform against a closed, credential-free loopback fixture, never Cloud.
const fixtureACLUserPath = "/v1/acl/users/42"
const fixtureACLUsersPath = "/v1/acl/users"

func TestACLUserManagedLifecycleLoopback(t *testing.T) {
	t.Run("create_rejected", func(t *testing.T) { runACLUserFixture(t, "rejected") })
	t.Run("accepted_then_503", func(t *testing.T) { runACLUserFixture(t, fixtureUncertainMode) })
	t.Run("lifecycle", func(t *testing.T) { runACLUserFixture(t, "success") })
}

func runACLUserFixture(t *testing.T, mode string) { //nolint:gocyclo // The fixture checks the complete ordered lifecycle.
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	var mu sync.Mutex
	reads, writes := 0, 0
	exists, unexpected := false, false
	rejectCreate, rejectUpdate, rejectDelete := mode != "success", true, true
	createRequests, deleteRequests := 0, 0
	rule := "fixture-rule-a"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			reads++
		} else {
			writes++
		}
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			unexpected = true
			http.Error(w, "fixture credentials required", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch {
		case r.Method == http.MethodPost && r.URL.Path == fixtureACLUsersPath && !exists:
			createRequests++
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body[fixtureNameKey] != "fixture-acl-user" || body["role"] != rule || body["password"] != "FixtureOnly1!" {
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
		case r.Method == http.MethodGet && r.URL.Path == fixtureACLUserPath && exists:
			response = map[string]any{"id": 42, fixtureNameKey: "fixture-acl-user", "role": rule, fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodPut && r.URL.Path == fixtureACLUserPath && exists:
			if rejectUpdate {
				rejectUpdate = false
				http.Error(w, "injected update rejection", http.StatusForbidden)
				return
			}
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["role"] != fixtureNextRole {
				unexpected = true
				http.Error(w, "bad update", http.StatusBadRequest)
				return
			}
			rule = fixtureNextRole
			response = map[string]any{fixtureTaskKey: "update"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/update":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureACLUserPath && exists:
			deleteRequests++
			if rejectDelete {
				rejectDelete = false
				http.Error(w, "injected delete rejection", http.StatusForbidden)
				return
			}
			exists = false
			response = map[string]any{fixtureTaskKey: "delete"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/delete":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == fixtureACLUserPath && !exists:
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == fixtureACLUsersPath:
			rows := []any{map[string]any{"id": 99, fixtureNameKey: "foreign-user", fixtureStatusKey: fixtureActive}}
			if exists {
				rows = append(rows, map[string]any{"id": 42, fixtureNameKey: "fixture-acl-user", "role": rule, fixtureStatusKey: fixtureActive})
			}
			response = map[string]any{"users": rows}
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
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := acl.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	name := fixtureName
	value := rule
	resourceName := "fixture-acl-user"
	item := &acl.User{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("fixture-acl-user-uid")}}
	item.Spec.ForProvider.Name = &resourceName
	item.Spec.ForProvider.Role = &value
	item.Spec.ForProvider.PasswordSecretRef = xpv2.SecretKeySelector{SecretReference: xpv2.SecretReference{Name: "fixture-password", Namespace: "fixture"}, Key: "password"}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "fixture-password", Namespace: "fixture"}, Data: map[string][]byte{"password": []byte("FixtureOnly1!")}}
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item, secret).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	store := newTrackingNativeStore()
	spy := &spyExecutor{Interface: kexec.New(), tracked: store}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_acl_user"], ujcontroller.WithCallbackProvider(callback))
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(acl.User_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
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
		if exists != (mode == fixtureUncertainMode) || unexpected || createRequests != 1 || deleteRequests != 0 {
			mu.Unlock()
			t.Fatal("failed create was replayed or fake outcome changed")
		}
		mu.Unlock()
		verifyForeignACLInventory(t, server.URL+fixtureACLUsersPath, "users", "foreign-user", "fixture-acl-user", mode == fixtureUncertainMode)
		return
	}
	await("terraform apply ", false)
	for i := 0; i < 5; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		current := &acl.User{}
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
	item.Spec.ForProvider.Role = &updated
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
	current := &acl.User{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.AtProvider.ID == nil || *current.Status.AtProvider.ID != "42" {
		t.Fatal("ACL ID changed")
	}
	mu.Lock()
	readsBefore, writesBefore := reads, writes
	mu.Unlock()
	observed := &acl.User{ObjectMeta: metav1.ObjectMeta{Name: "observe-user", UID: types.UID("observe-user-uid")}, Spec: current.Spec}
	verifyACLObserveOnly(t, observed, scheme, "rediscloud_acl_user", server.URL, secret.DeepCopy())
	mu.Lock()
	if reads <= readsBefore || writes != writesBefore {
		mu.Unlock()
		t.Fatal("observe-only import was not GET-only")
	}
	readsBefore = reads
	mu.Unlock()
	for i := 0; i < 3; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(time.Second)
	mu.Lock()
	if reads <= readsBefore || writes != writesBefore {
		mu.Unlock()
		t.Fatal("unchanged reconcile did not perform GET-only observation")
	}
	mu.Unlock()
	if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
		t.Fatal(err)
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
		if apierrors.IsNotFound(kube.Get(context.Background(), types.NamespacedName{Name: name}, &acl.User{})) {
			break
		}
		if i == 29 {
			item := &acl.User{}
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
	for path, status := range map[string]int{fixtureACLUserPath: http.StatusNotFound, fixtureACLUsersPath: http.StatusOK} {
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
				} `json:"users"`
			}
			err = json.NewDecoder(res.Body).Decode(&list)
			if err != nil || len(list.Rules) != 1 || list.Rules[0].ID != 99 || list.Rules[0].Name != "foreign-user" {
				_ = res.Body.Close()
				t.Fatal("fake ACL inventory incomplete or nonempty")
			}
		}
		_ = res.Body.Close()
	}
	if _, err := os.Stat(filepath.Join(dir, "fixture-acl-user-uid")); !os.IsNotExist(err) {
		t.Fatal("workspace remains")
	}
}
