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
	fixtureSubscriptionIDKey = "subscriptionId"
	fixtureTFCLIArgsInit     = "TF_CLI_ARGS_init"
	fixtureTFCLIArgsApply    = "TF_CLI_ARGS_apply"
	fixtureTFCLIArgsPlan     = "TF_CLI_ARGS_plan"
	fixtureTFCLIArgsDestroy  = "TF_CLI_ARGS_destroy"
	fixtureCreateTask        = "create"
	fixtureUpdateTask        = "update"
	fixtureDeleteTask        = "delete"
	fixtureCreateTaskPath    = "/v1/tasks/create"
	fixtureUpdateTaskPath    = "/v1/tasks/update"
	fixtureDeleteTaskPath    = "/v1/tasks/delete"
	fixtureDatabasesKey      = "databases"
	fixtureTFCLIArgs         = "TF_CLI_ARGS"
	fixtureNextRole          = "fixture-rule-b"
	fixtureSourceIPsKey      = "sourceIps"
	fixtureProtocolKey       = "protocol"
	fixtureProtocolValue     = "redis"
	fixtureSecurityKey       = "security"
	fixtureModulesKey        = "modules"
	fixtureHTTPProxy         = "HTTP_PROXY"
	fixtureHTTPProxyLower    = "http_proxy"
	fixtureHTTPSProxyLower   = "https_proxy"
	fixtureAllProxyLower     = "all_proxy"
	fixturePersistenceKey    = "dataPersistence"
	fixtureHTTPSProxy        = "HTTPS_PROXY"
	fixtureEvictionKey       = "dataEvictionPolicy"
	fixtureResponseKey       = "response"
	fixtureRegion            = "us-central1"
	fixtureAPIKey            = "api_key"
	fixtureSecretKey         = "secret_key"
	fixtureURLKey            = "url"
	fixtureProviderSource    = "RedisLabs/rediscloud"
	fixtureProviderVersion   = "2.20.0"
	fixtureTaskKey           = "taskId"
	fixtureTaskComplete      = "processing-completed"
	fixtureParentPath        = "/v1/fixed/subscriptions/1"
	fixtureProviderKey       = "provider"
	fixtureProviderName      = "rediscloud"
	fixtureExternalID        = "1/2"
	fixtureNameKey           = "name"
	fixtureStatusKey         = "status"
	fixtureDatabasePath      = "/v1/fixed/subscriptions/1/databases/2"
	fixtureActive            = "active"
	fixtureVolatileLRU       = "volatile-lru"
	fixtureDatabaseIDKey     = "databaseId"
	fixtureTagsKey           = "tags"
	fixtureTerraformVersion  = "1.5.7"
	fixtureProParentPath     = "/v1/subscriptions/1"
	fixtureTaskResourceKey   = "resourceId"
	fixturePaymentType       = "credit-card"
	fixtureGCP               = "GCP"
	fixtureAllProxy          = "ALL_PROXY"
	fixtureProDBsPath        = "/v1/subscriptions/1/databases"
	fixtureProDBName         = "fixture-pro-db"
	fixtureAlertsKey         = "alerts"
	fixtureProDBID           = "1/42"
	fixtureThroughputBy      = "operations-per-second"
	fixtureReplicationKey    = "replication"
	fixtureNoPersistence     = "none"
	fixtureCaseSuccess       = "success"
	fixtureStringTrue        = "true"
)

func requireHostedFixture(t *testing.T) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue {
		t.Skip("provider fixture requires GitHub Actions")
	}
}

// The parent is pre-existing in this incremental fake: only managed DB create
// is exercised. No Cloud account, account admission, or full teardown is proven.
func TestEssentialsManagedCreateLoopback(t *testing.T) { //nolint:gocyclo // The integration case keeps its local server and lifecycle together.
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	var mu sync.Mutex
	created, deleted, parentCreated, parentDeleted, unexpected := false, false, false, false, false
	fixtureEviction := desiredEviction
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/fixed/subscriptions":
			rows := []any{}
			if parentCreated {
				rows = append(rows, map[string]any{"id": 1, fixtureNameKey: fixtureName})
			}
			response = map[string]any{"subscriptions": rows}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/fixed/subscriptions" && !parentCreated:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body[fixtureNameKey] != fixtureName || body["planId"] != float64(1) {
				unexpected = true
				http.Error(w, "wrong parent fixture body", http.StatusBadRequest)
				return
			}
			parentCreated = true
			response = map[string]any{fixtureTaskKey: "parent-create"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/parent-create":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureTaskResourceKey: 1}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureParentPath && parentCreated:
			response = map[string]any{"id": 1, fixtureNameKey: fixtureName, fixtureStatusKey: fixtureActive, "planId": 1, "paymentMethodType": fixturePaymentType, "creationDate": "2025-01-01T00:00:00Z"}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureParentPath && parentCreated && !created:
			parentCreated, parentDeleted = false, true
			response = map[string]any{fixtureTaskKey: "parent-delete"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/parent-delete" && parentDeleted:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == fixtureParentPath && parentDeleted:
			http.NotFound(w, r)
			return
		case r.Method == http.MethodPost && r.URL.Path == "/v1/fixed/subscriptions/1/databases" && parentCreated && !created:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body[fixtureNameKey] != fixtureName || body[fixtureEvictionKey] != desiredEviction || body[fixturePersistenceKey] != fixtureNoPersistence || body[fixtureReplicationKey] != false {
				unexpected = true
				http.Error(w, "wrong fixture body", http.StatusBadRequest)
				return
			}
			created = true
			response = map[string]any{fixtureTaskKey: fixtureCreateTask}
		case r.Method == http.MethodGet && r.URL.Path == fixtureCreateTaskPath:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureTaskResourceKey: 2}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureDatabasePath && created:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || (body[fixtureEvictionKey] != desiredEviction && body[fixtureEvictionKey] != fixtureVolatileLRU) {
				unexpected = true
				http.Error(w, "wrong fixture update", http.StatusBadRequest)
				return
			}
			fixtureEviction = body[fixtureEvictionKey].(string)
			response = map[string]any{fixtureTaskKey: fixtureUpdateTask}
		case r.Method == http.MethodGet && r.URL.Path == fixtureUpdateTaskPath:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureDatabasePath && created:
			created, deleted = false, true
			response = map[string]any{fixtureTaskKey: fixtureDeleteTask}
		case r.Method == http.MethodGet && r.URL.Path == fixtureDeleteTaskPath && deleted:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == fixtureDatabasePath && deleted:
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == fixtureDatabasePath && created:
			response = map[string]any{fixtureDatabaseIDKey: 2, fixtureNameKey: fixtureName, fixtureProtocolKey: fixtureProtocolValue, fixtureProviderKey: fixtureGCP, "region": fixtureRegion, fixtureStatusKey: fixtureActive, fixturePersistenceKey: fixtureNoPersistence, fixtureEvictionKey: fixtureEviction, fixtureReplicationKey: false, "activatedOn": "2025-01-01T00:00:00Z", fixtureSecurityKey: map[string]any{"defaultUserEnabled": true, fixtureSourceIPsKey: []string{"0.0.0.0/0"}}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureDatabasePath+"/tags" && created:
			response = map[string]any{fixtureTagsKey: []any{}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureDatabasePath+"/tags" && created:
			response = map[string]any{fixtureTagsKey: []any{}}
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
	t.Setenv("TMPDIR", dir)
	t.Setenv("TF_CLI_CONFIG_FILE", filepath.Join(dir, "offline.tfrc"))
	for _, key := range []string{fixtureTFCLIArgs, fixtureTFCLIArgsInit, fixtureTFCLIArgsApply, fixtureTFCLIArgsPlan, fixtureTFCLIArgsDestroy} {
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
	mirror := fmt.Sprintf("provider_installation {\n filesystem_mirror { path = %q }\n}\n", plugins)
	if err := os.WriteFile(filepath.Join(dir, "offline.tfrc"), []byte(mirror), 0o600); err != nil {
		t.Fatal(err)
	}
	assertEmptyFakeInventory(t, server.URL)
	t.Log("SIMULATED: authenticated fake baseline inventory empty")
	parent := createManagedParent(t, server.URL, schemeForParent(t))
	t.Log("SIMULATED: managed fake parent created and observed")
	store := newTrackingNativeStore()
	spy := &spyExecutor{Interface: kexec.New(), tracked: store}
	var err error
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
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_essentials_database"], ujcontroller.WithCallbackProvider(callback))
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(api.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
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
	updated := fixtureVolatileLRU
	item.Spec.ForProvider.DataEviction = &updated
	if err = kube.Update(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	verifyManagedDrift(t, rec, spy, callback, false)
	mu.Lock()
	if fixtureEviction != updated || !created || !parentCreated {
		mu.Unlock()
		t.Fatal("fake update or identities differ")
	}
	mu.Unlock()
	assertManagedFakeUpdate(t, server.URL, updated)
	t.Log("SIMULATED: managed fake database update read back with stable ID")
	current := &api.Database{}
	if err = kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.AtProvider.ID == nil || *current.Status.AtProvider.ID != fixtureExternalID {
		t.Fatal("managed database identity changed during fake update")
	}
	item = current
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
			assertManagedObjectGone(t, rec, kube, filepath.Join(dir, "fixture-uid"))
			parent.destroy(t)
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
					assertManagedObjectGone(t, rec, kube, filepath.Join(dir, "fixture-uid"))
					parent.destroy(t)
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
		fixtureParentPath:   {"id", 1},
		fixtureDatabasePath: {fixtureDatabaseIDKey, 2},
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
	t.Log("SIMULATED: managed fake child and parent both read active")
}

func assertManagedFakeUpdate(t *testing.T, base, eviction string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+fixtureDatabasePath, nil)
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
	if err != nil || res.StatusCode != http.StatusOK || body[fixtureDatabaseIDKey] != float64(2) || body[fixtureEvictionKey] != eviction {
		t.Fatal("fake update readback or identity mismatch")
	}
}

func assertManagedFakeDelete(t *testing.T, base string, mu *sync.Mutex, deleted, unexpected *bool) {
	t.Helper()
	for path, wanted := range map[string]int{fixtureDatabasePath: http.StatusNotFound, fixtureParentPath: http.StatusOK} {
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
	t.Log("SIMULATED: managed fake child 404 while fake parent 200")
}

func assertManagedObjectGone(t *testing.T, rec *managed.Reconciler, kube client.Client, workspacePath string) {
	t.Helper()
	for i := 0; i < 5; i++ {
		_, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}})
		if err != nil {
			t.Fatal(err)
		}
		current := &api.Database{}
		err = kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current)
		if apierrors.IsNotFound(err) {
			if _, statErr := os.Stat(workspacePath); !os.IsNotExist(statErr) {
				t.Fatal("fake child workspace remains or cannot be checked")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("managed resource still exists after delete")
}

type managedParentFixture struct {
	rec           *managed.Reconciler
	kube          client.Client
	callback      *fakeCallback
	spy           *spyExecutor
	url           string
	workspacePath string
}

func schemeForParent(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func createManagedParent(t *testing.T, url string, scheme *runtime.Scheme) *managedParentFixture {
	t.Helper()
	store := newTrackingNativeStore()
	spy := &spyExecutor{Interface: kexec.New(), tracked: store}
	var err error
	item := &api.Subscription{ObjectMeta: metav1.ObjectMeta{Name: fixtureName, UID: types.UID("fixture-parent-uid")}}
	name, plan := fixtureName, float64(1)
	item.Spec.ForProvider.Name, item.Spec.ForProvider.PlanID = &name, &plan
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: url + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_essentials_subscription"], ujcontroller.WithCallbackProvider(callback))
	parent := &managedParentFixture{rec: managed.NewReconciler(mgr, xpresource.ManagedKind(api.Subscription_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName)))), kube: kube, callback: callback, spy: spy, url: url, workspacePath: filepath.Join(os.TempDir(), "fixture-parent-uid")}
	parent.await(t, "terraform apply ")
	for i := 0; i < 5; i++ {
		if _, err = parent.rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
			t.Fatal(err)
		}
		current := &api.Subscription{}
		if err = kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); err != nil {
			t.Fatal(err)
		}
		if current.Status.AtProvider.ID != nil && *current.Status.AtProvider.ID == "1" && len(current.Finalizers) > 0 {
			return parent
		}
	}
	t.Fatal("managed parent identity/finalizer missing")
	return nil
}

func (p *managedParentFixture) await(t *testing.T, command string) { //nolint:gocyclo // Exercise bounded asynchronous reconcile transitions.
	t.Helper()
	for i := 0; i < 8; i++ {
		if _, err := p.rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-p.callback.done:
			if err != nil {
				t.Fatalf("parent callback: %v", err)
			}
			return
		case <-time.After(2 * time.Second):
		}
		for _, actual := range p.spy.snapshot() {
			if strings.Contains(actual, command) && !strings.Contains(actual, "-refresh-only") {
				select {
				case err := <-p.callback.done:
					if err != nil {
						t.Fatalf("parent callback: %v", err)
					}
					return
				case <-time.After(90 * time.Second):
					t.Fatal("parent callback timed out")
				}
			}
		}
	}
	current := &api.Subscription{}
	_ = p.kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current)
	t.Fatalf("parent callback missing: status=%#v annotations=%#v", current.Status, current.Annotations)
}

func (p *managedParentFixture) destroy(t *testing.T) { //nolint:gocyclo // Check each deletion and inventory boundary.
	t.Helper()
	current := &api.Subscription{}
	if err := p.kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); err != nil {
		t.Fatal(err)
	}
	if err := p.kube.Delete(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	p.await(t, "terraform destroy ")
	for i := 0; i < 5; i++ {
		if _, err := p.rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
			t.Fatal(err)
		}
		err := p.kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, &api.Subscription{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if i == 4 {
			t.Fatal("managed parent still exists")
		}
	}
	req, err := http.NewRequest(http.MethodGet, p.url+fixtureParentPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", dummyCredential)
	req.Header.Set("X-Api-Secret-Key", dummyCredential)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("parent read after delete = %d", resp.StatusCode)
	}
	assertEmptyFakeInventory(t, p.url)
	if _, err := os.Stat(p.workspacePath); !os.IsNotExist(err) {
		t.Fatal("fake parent workspace remains or cannot be checked")
	}
	t.Log("SIMULATED: managed fake parent 404 and fake inventory empty")
}

func assertEmptyFakeInventory(t *testing.T, url string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+"/v1/fixed/subscriptions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", dummyCredential)
	req.Header.Set("X-Api-Secret-Key", dummyCredential)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Subscriptions []any `json:"subscriptions"`
	}
	err = json.NewDecoder(resp.Body).Decode(&body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || body.Subscriptions == nil || len(body.Subscriptions) != 0 {
		t.Fatal("fake subscription inventory is not empty or malformed")
	}
}
