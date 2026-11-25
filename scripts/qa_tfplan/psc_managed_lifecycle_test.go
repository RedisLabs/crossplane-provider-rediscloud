package qatfplan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	api "github.com/RedisLabs/provider-rediscloud/apis/private/v1beta1"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	fixtureResourceKey = "resource"
)

// This fixture starts with an existing fake Pro parent. All writes target the
// local fake PSC service; no AWS, Cloud account or live lock is exercised.
func TestPSCManagedServiceLifecycleLoopback(t *testing.T) {
	runPSCManagedService(t, false)
}

func TestPSCManagedServiceFailureLoopback(t *testing.T) {
	runPSCManagedService(t, true)
}

func runPSCManagedService(t *testing.T, injectFailure bool) { //nolint:gocyclo // Keep the isolated fake API and managed lifecycle together.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" && plugins == "" {
		t.Skip("pinned Terraform fixture not configured")
	}
	if tf == "" || plugins == "" {
		t.Fatal("pinned Terraform and provider are required")
	}
	const path = "/v1/subscriptions/1/private-service-connect"
	var mu sync.Mutex
	exists, created, deleted, unexpected, failCreate := false, false, false, false, injectFailure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			unexpected = true
			http.Error(w, "fixture auth required", http.StatusForbidden)
			return
		}
		var response any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == fixtureProParentPath:
			response = map[string]any{"id": 1, "name": "fixture-parent", fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodGet && r.URL.Path == path && !exists:
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == path && exists:
			response = map[string]any{fixtureTaskKey: "get-link"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-link" && exists:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureResourceKey: map[string]any{"id": 7, fixtureStatusKey: fixtureActive}}}
		case r.Method == http.MethodPost && r.URL.Path == path && !exists && !created:
			if failCreate {
				failCreate = false
				http.Error(w, "injected fixture failure", http.StatusBadRequest)
				return
			}
			exists, created = true, true
			response = map[string]any{fixtureTaskKey: "create-link"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/create-link" && created:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{"resourceId": 7}}
		case r.Method == http.MethodDelete && r.URL.Path == path && exists && !deleted:
			exists, deleted = false, true
			response = map[string]any{fixtureTaskKey: "delete-link"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/delete-link" && deleted:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		default:
			unexpected = true
			http.Error(w, "unexpected fixture request", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TF_CLI_CONFIG_FILE", filepath.Join(dir, "offline.tfrc"))
	for _, key := range []string{"TF_CLI_ARGS", "TF_CLI_ARGS_init", "TF_CLI_ARGS_apply", "TF_CLI_ARGS_plan", "TF_CLI_ARGS_destroy"} {
		t.Setenv(key, "")
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "http://127.0.0.1:1")
	}
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	t.Setenv("REDISCLOUD_URL", server.URL+"/v1")
	t.Setenv("REDISCLOUD_ACCESS_KEY", dummyCredential)
	t.Setenv("REDISCLOUD_SECRET_KEY", dummyCredential)
	t.Setenv("PATH", filepath.Dir(tf)+string(os.PathListSeparator)+os.Getenv("PATH"))
	mirror := fmt.Sprintf("provider_installation {\n filesystem_mirror { path = %q }\n}\n", plugins)
	if err := os.WriteFile(filepath.Join(dir, "offline.tfrc"), []byte(mirror), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newTrackingNativeStore()
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	item := &api.ServiceConnect{ObjectMeta: metav1.ObjectMeta{Name: "fixture-psc", UID: types.UID("fixture-psc-uid")}}
	sub := "1"
	item.Spec.ForProvider.SubscriptionID = &sub
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_private_service_connect"], ujcontroller.WithCallbackProvider(callback))
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(api.ServiceConnect_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: item.Name}}
	await := func(label string, wantError bool) {
		t.Helper()
		for i := 0; i < 8; i++ {
			if _, err := rec.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-callback.done:
				if (err != nil) != wantError {
					t.Fatalf("%s failed: %v", label, err)
				}
				return
			case <-time.After(2 * time.Second):
			}
			if store.current != nil && store.current.LastOperation != nil && store.current.LastOperation.IsRunning() {
				select {
				case err := <-callback.done:
					if (err != nil) != wantError {
						t.Fatalf("%s failed: %v", label, err)
					}
					return
				case <-time.After(180 * time.Second):
					t.Fatalf("%s timed out", label)
				}
			}
		}
		t.Fatalf("%s callback not observed", label)
	}
	if injectFailure {
		await("injected create failure", true)
		mu.Lock()
		if exists || created || unexpected {
			mu.Unlock()
			t.Fatal("failed create unexpectedly mutated fixture")
		}
		mu.Unlock()
		for i := 0; i < 3; i++ {
			if _, err := rec.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if exists || created || unexpected {
			t.Fatal("uncertain create was automatically replayed")
		}
		t.Log("SIMULATED: service create failure remained fenced; recovery requires an independent absence check")
		return
	}
	await("create", false)
	for i := 0; i < 6; i++ {
		if _, err := rec.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if err := kube.Get(context.Background(), req.NamespacedName, item); err != nil {
			t.Fatal(err)
		}
		if item.Status.AtProvider.ID != nil && *item.Status.AtProvider.ID == "1/7" && len(item.Finalizers) != 0 {
			break
		}
	}
	if item.Status.AtProvider.ID == nil || *item.Status.AtProvider.ID != "1/7" || len(item.Finalizers) == 0 {
		t.Fatal("fake PSC identity/finalizer missing")
	}
	mu.Lock()
	if !exists || !created || unexpected {
		mu.Unlock()
		t.Fatal("unexpected fake create")
	}
	mu.Unlock()
	t.Log("SIMULATED: managed fake PSC service created and read; fake parent present")
	if err := kube.Delete(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	await("delete", false)
	mu.Lock()
	if exists || !deleted || unexpected {
		mu.Unlock()
		t.Fatal("unexpected fake delete")
	}
	mu.Unlock()
	for target, expected := range map[string]int{path: http.StatusNotFound, fixtureProParentPath: http.StatusOK} {
		request, err := http.NewRequest(http.MethodGet, server.URL+target, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Api-Key", dummyCredential)
		request.Header.Set("X-Api-Secret-Key", dummyCredential)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != expected {
			t.Fatalf("fake read %s status=%d", target, response.StatusCode)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := rec.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		err := kube.Get(context.Background(), req.NamespacedName, &api.ServiceConnect{})
		if apierrors.IsNotFound(err) {
			if _, statErr := os.Stat(filepath.Join(dir, "fixture-psc-uid")); !os.IsNotExist(statErr) {
				t.Fatal("fake workspace remains or cannot be checked")
			}
			t.Log("SIMULATED: fake PSC service absent with fake parent still present")
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("managed PSC object still exists after delete")
}
