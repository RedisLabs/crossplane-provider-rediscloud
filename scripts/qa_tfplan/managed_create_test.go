package qatfplan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/RedisLabs/provider-rediscloud/apis/essentials/v1beta1"
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

const (
	fixtureExternalID    = "1/2"
	fixtureNameKey       = "name"
	fixtureStatusKey     = "status"
	fixtureDatabasePath  = "/v1/fixed/subscriptions/1/databases/2"
	fixtureActive        = "active"
	fixtureNoPersistence = "none"
)

// The parent is pre-existing in this incremental fake: only managed DB create
// is exercised. No Cloud account, account admission, or full teardown is proven.
func TestEssentialsManagedCreateLoopback(t *testing.T) { //nolint:gocyclo // The integration case keeps its local server and lifecycle together.
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	var mu sync.Mutex
	created, deleted, unexpected := false, false, false
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/fixed/subscriptions/1":
			response = map[string]any{"id": 1, fixtureNameKey: fixtureName, fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/fixed/subscriptions/1/databases" && !created:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body[fixtureNameKey] != fixtureName || body["dataEvictionPolicy"] != desiredEviction || body["dataPersistence"] != fixtureNoPersistence || body["replication"] != false {
				unexpected = true
				http.Error(w, "wrong fixture body", http.StatusBadRequest)
				return
			}
			created = true
			response = map[string]any{"taskId": "create"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/create":
			response = map[string]any{fixtureStatusKey: "processing-completed", "response": map[string]any{"resourceId": 2}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureDatabasePath && created:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["dataEvictionPolicy"] != desiredEviction {
				unexpected = true
				http.Error(w, "wrong fixture update", http.StatusBadRequest)
				return
			}
			response = map[string]any{"taskId": "update"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/update":
			response = map[string]any{fixtureStatusKey: "processing-completed"}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureDatabasePath && created:
			created, deleted = false, true
			response = map[string]any{"taskId": "delete"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/delete" && deleted:
			response = map[string]any{fixtureStatusKey: "processing-completed"}
		case r.Method == http.MethodGet && r.URL.Path == fixtureDatabasePath && deleted:
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == fixtureDatabasePath && created:
			response = map[string]any{"databaseId": 2, fixtureNameKey: fixtureName, "protocol": "redis", "provider": "GCP", "region": "us-central1", fixtureStatusKey: fixtureActive, "dataPersistence": fixtureNoPersistence, "dataEvictionPolicy": desiredEviction, "replication": false, "activatedOn": "2025-01-01T00:00:00Z", "security": map[string]any{"defaultUserEnabled": true, "sourceIps": []string{"0.0.0.0/0"}}, "modules": []any{}, "alerts": []any{}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureDatabasePath+"/tags" && created:
			response = map[string]any{"tags": []any{}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureDatabasePath+"/tags" && created:
			response = map[string]any{"tags": []any{}}
		default:
			unexpected = true
			http.Error(w, "unexpected fixture request", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("TF_CLI_CONFIG_FILE", filepath.Join(dir, "empty.tfrc"))
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
	if err := os.WriteFile(filepath.Join(dir, "empty.tfrc"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"terraform": map[string]any{"required_providers": map[string]any{"rediscloud": map[string]any{"source": "RedisLabs/rediscloud", "version": "2.20.0"}}}, "provider": map[string]any{"rediscloud": map[string]any{"api_key": dummyCredential, "secret_key": dummyCredential, "url": server.URL + "/v1"}}, "resource": map[string]any{"rediscloud_essentials_database": map[string]any{"test": map[string]any{"subscription_id": 1, fixtureNameKey: fixtureName, "data_eviction": desiredEviction, "data_persistence": fixtureNoPersistence, "replication": false}}}}
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "main.tf.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	init := exec.Command(tf, "init", "-no-color", "-input=false", "-plugin-dir="+plugins)
	init.Dir = dir
	if out, initErr := init.CombinedOutput(); initErr != nil {
		t.Fatalf("fixture init failed: %v\n%s", initErr, out)
	}
	spy := &spyExecutor{Interface: kexec.New()}
	ws := terraform.NewWorkspace(dir, terraform.WithExecutor(spy), terraform.WithFilterFn(func(s string) string { return s }))
	scheme := runtime.NewScheme()
	if err = api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	item := &api.Database{ObjectMeta: metav1.ObjectMeta{Name: fixtureName, UID: types.UID("fixture-uid")}}
	name, eviction, persistence, replication, sub := fixtureName, desiredEviction, fixtureNoPersistence, false, float64(1)
	item.Spec.ForProvider.Name = &name
	item.Spec.ForProvider.DataEviction = &eviction
	item.Spec.ForProvider.DataPersistence = &persistence
	item.Spec.ForProvider.Replication = &replication
	item.Spec.ForProvider.SubscriptionID = &sub
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, fixtureStore{ws}, setup, providerconfig.GetProvider().Resources["rediscloud_essentials_database"], ujcontroller.WithCallbackProvider(callback))
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(api.Database_GroupVersionKind), managed.WithExternalConnector(connector))
	createdDone := false
createLoop:
	for i := 0; i < 8; i++ {
		if _, err = rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
			t.Fatal(err)
		}
		select {
		case err = <-callback.done:
			if err != nil {
				t.Fatalf("managed create failed: %v", err)
			}
			assertManagedFakeCreate(t, server.URL, &mu, &created, &unexpected)
			createdDone = true
			break createLoop
		case <-time.After(2 * time.Second):
		}
		for _, command := range spy.snapshot() {
			if strings.Contains(command, "terraform apply ") && !strings.Contains(command, "-refresh-only") {
				select {
				case err = <-callback.done:
					if err != nil {
						t.Fatalf("managed create failed: %v", err)
					}
					assertManagedFakeCreate(t, server.URL, &mu, &created, &unexpected)
					createdDone = true
					break createLoop
				case <-time.After(90 * time.Second):
					t.Fatal("managed create callback timed out")
				}
			}
		}
	}
	if !createdDone {
		t.Fatalf("managed create callback was not observed: %v", spy.snapshot())
	}
	for i := 0; i < 5; i++ {
		if _, err = rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
			t.Fatal(err)
		}
		current := &api.Database{}
		if err = kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); err != nil {
			t.Fatal(err)
		}
		if current.Status.AtProvider.ID != nil && *current.Status.AtProvider.ID == fixtureExternalID {
			item = current
			break
		}
	}
	if item.Status.AtProvider.ID == nil || *item.Status.AtProvider.ID != fixtureExternalID || len(item.Finalizers) == 0 {
		t.Fatal("managed database identity/finalizer not observed")
	}
	if err = kube.Delete(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err = rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
			t.Fatal(err)
		}
		select {
		case err = <-callback.done:
			if err != nil {
				t.Fatalf("managed delete failed: %v", err)
			}
			assertManagedFakeDelete(t, server.URL, &mu, &deleted, &unexpected)
			assertManagedObjectGone(t, rec, kube)
			return
		case <-time.After(2 * time.Second):
		}
		for _, command := range spy.snapshot() {
			if strings.Contains(command, "terraform destroy ") {
				select {
				case err = <-callback.done:
					if err != nil {
						t.Fatalf("managed delete failed: %v", err)
					}
					assertManagedFakeDelete(t, server.URL, &mu, &deleted, &unexpected)
					assertManagedObjectGone(t, rec, kube)
					return
				case <-time.After(90 * time.Second):
					t.Fatal("managed delete callback timed out")
				}
			}
		}
	}
	t.Fatalf("managed delete callback was not observed: %v", spy.snapshot())
}

func assertManagedFakeCreate(t *testing.T, base string, mu *sync.Mutex, created, unexpected *bool) {
	t.Helper()
	for path, key := range map[string]struct {
		key string
		id  float64
	}{
		"/v1/fixed/subscriptions/1": {"id", 1},
		fixtureDatabasePath:         {"databaseId", 2},
	} {
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Api-Key", dummyCredential)
		req.Header.Set("X-Api-Secret-Key", dummyCredential)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		err = json.NewDecoder(res.Body).Decode(&body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK || body[key.key] != key.id || body[fixtureStatusKey] != fixtureActive {
			t.Fatalf("fake authenticated read failed for %s", path)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !*created || *unexpected {
		t.Fatalf("create=%v unexpected=%v", *created, *unexpected)
	}
}

func assertManagedFakeDelete(t *testing.T, base string, mu *sync.Mutex, deleted, unexpected *bool) {
	t.Helper()
	for path, wanted := range map[string]int{fixtureDatabasePath: http.StatusNotFound, "/v1/fixed/subscriptions/1": http.StatusOK} {
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Api-Key", dummyCredential)
		req.Header.Set("X-Api-Secret-Key", dummyCredential)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != wanted {
			t.Fatalf("fake authenticated delete read %s = %d", path, res.StatusCode)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !*deleted || *unexpected {
		t.Fatalf("delete=%v unexpected=%v", *deleted, *unexpected)
	}
}

func assertManagedObjectGone(t *testing.T, rec *managed.Reconciler, kube client.Client) {
	t.Helper()
	for i := 0; i < 5; i++ {
		_, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}})
		if err != nil {
			t.Fatal(err)
		}
		current := &api.Database{}
		err = kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current)
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("managed resource still exists after delete")
}
