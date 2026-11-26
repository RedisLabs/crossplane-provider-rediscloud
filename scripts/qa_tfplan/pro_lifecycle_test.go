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

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	procontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/subscription/database"
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

// TestProDatabaseManagedLifecycleLoopback uses actual reconciliation and Terraform
// against loopback fake Cloud, with a pre-existing fake parent; it never contacts Cloud.
const fixtureProDatabasePath = "/v1/subscriptions/1/databases/42"

func TestProDatabaseManagedLifecycleLoopback(t *testing.T) {
	t.Run("lifecycle", func(t *testing.T) { runProDatabaseFixture(t, fixtureCaseSuccess) })
	t.Run("accepted_then_503", func(t *testing.T) { runProDatabaseFixture(t, "create_unknown") })
	t.Run("delete_accepted_then_503", func(t *testing.T) { runProDatabaseFixture(t, "delete_unknown") })
}

func TestProductProDeleteHold(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		enabled    bool
	}{{"default_off", fixtureCaseSuccess, false}, {fixtureCaseSuccess, fixtureCaseSuccess, true}, {"accepted_503", "delete_delayed", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("QA_PRODUCT_HOLD_ENVTEST", fixtureStringTrue)
			if tc.enabled {
				t.Setenv("QA_PRODUCT_HOLD_TEST", fixtureStringTrue)
			}
			runProDatabaseFixture(t, tc.mode)
		})
	}
}

func runProDatabaseFixture(t *testing.T, mode string) { //nolint:gocyclo // The fixture checks the complete ordered lifecycle.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	var mu sync.Mutex
	exists, unexpected := false, false
	createRequests, deleteRequests, childAbsentReads := 0, 0, 0
	beforeDeleteReconcileReads := 0
	rule := desiredEviction
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
		case r.Method == http.MethodGet && r.URL.Path == fixtureProParentPath:
			response = map[string]any{"id": 1, fixtureNameKey: "fixture-existing-parent", fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodPost && r.URL.Path == fixtureProDBsPath:
			createRequests++
			if exists {
				unexpected = true
				http.Error(w, "duplicate create", http.StatusConflict)
				return
			}
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body[fixtureNameKey] != fixtureProDBName || body[fixtureEvictionKey] != rule {
				unexpected = true
				http.Error(w, "bad create", http.StatusBadRequest)
				return
			}
			exists = true
			if mode == "create_unknown" {
				http.Error(w, "injected accepted create with unavailable response", http.StatusServiceUnavailable)
				return
			}
			response = map[string]any{fixtureTaskKey: fixtureCreateTask}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/databases/42/tags":
			response = map[string]any{fixtureTagsKey: []any{}}
		case r.Method == http.MethodPut && r.URL.Path == "/v1/subscriptions/1/databases/42/tags":
			response = map[string]any{fixtureTagsKey: []any{}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureCreateTaskPath:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureTaskResourceKey: 42}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDatabasePath && exists:
			response = map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureEvictionKey: rule, fixtureStatusKey: fixtureActive, fixtureProtocolKey: fixtureProtocolValue, fixturePersistenceKey: fixtureNoPersistence, fixtureReplicationKey: false, "memoryLimitInGb": 1, "throughputMeasurement": map[string]any{"by": fixtureThroughputBy, "value": 1000}, fixtureSecurityKey: map[string]any{"enableDefaultUser": true, "enableTls": false, "tlsClientAuthentication": false, fixtureSourceIPsKey: []any{}}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{}, "clustering": map[string]any{"regexRules": []any{}}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureProDatabasePath && exists:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				unexpected = true
				http.Error(w, "malformed update", http.StatusBadRequest)
				return
			}
			if policy, present := body[fixtureEvictionKey]; present {
				if policy != desiredEviction && policy != fixtureVolatileLRU {
					unexpected = true
					http.Error(w, "bad eviction policy", http.StatusBadRequest)
					return
				}
				rule = policy.(string)
			}
			response = map[string]any{fixtureTaskKey: fixtureUpdateTask}
		case r.Method == http.MethodGet && r.URL.Path == fixtureUpdateTaskPath:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureProDatabasePath:
			deleteRequests++
			if mode == "delete_delayed" {
				if deleteRequests != 1 {
					unexpected = true
				}
				http.Error(w, "injected pending delete", http.StatusServiceUnavailable)
				return
			}
			if !exists {
				unexpected = true
				http.NotFound(w, r)
				return
			}
			exists = false
			if mode == "delete_unknown" {
				http.Error(w, "injected accepted delete with unavailable response", http.StatusServiceUnavailable)
				return
			}
			response = map[string]any{fixtureTaskKey: fixtureDeleteTask}
		case r.Method == http.MethodGet && r.URL.Path == fixtureDeleteTaskPath:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDatabasePath && !exists:
			childAbsentReads++
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDBsPath:
			rows := []any{}
			if exists {
				rows = append(rows, map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureEvictionKey: rule, fixtureStatusKey: fixtureActive, fixtureProtocolKey: fixtureProtocolValue, fixturePersistenceKey: fixtureNoPersistence, fixtureReplicationKey: false, "memoryLimitInGb": 1, "throughputMeasurement": map[string]any{"by": fixtureThroughputBy, "value": 1000}, fixtureSecurityKey: map[string]any{"enableDefaultUser": true, "enableTls": false, "tlsClientAuthentication": false, fixtureSourceIPsKey: []any{}}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{}, "clustering": map[string]any{"regexRules": []any{}}})
			}
			response = map[string]any{fixtureDatabasesKey: rows}
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
	if err := os.WriteFile(filepath.Join(dir, "offline.tfrc"), []byte(fmt.Sprintf("provider_installation {\n filesystem_mirror { path = %q }\n}\n", plugins)), 0o600); err != nil {
		t.Fatal(err)
	}
	scheme := runtime.NewScheme()
	if err := pro.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	name, value := fixtureName, rule
	resourceName := fixtureProDBName
	item := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("fixture-pro-db-uid")}}
	item.Spec.ForProvider.Name = &resourceName
	item.Spec.ForProvider.DataEviction = &value
	parent, memory, by, throughput, persistence, replication := float64(1), float64(1), fixtureThroughputBy, float64(1000), fixtureNoPersistence, false
	item.Spec.ForProvider.SubscriptionID = &parent
	item.Spec.ForProvider.MemoryLimitInGb = &memory
	item.Spec.ForProvider.ThroughputMeasurementBy = &by
	item.Spec.ForProvider.ThroughputMeasurementValue = &throughput
	item.Spec.ForProvider.DataPersistence = &persistence
	item.Spec.ForProvider.Replication = &replication
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	item.Spec.ProviderConfigReference = &xpv2.Reference{Name: "fixture-config"}
	var kube client.Client
	if os.Getenv("QA_PRODUCT_HOLD_ENVTEST") == fixtureStringTrue {
		kube = newProEnvtestClient(t, scheme, item)
	} else {
		kube = cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	}
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	store := newTrackingNativeStore()
	spy := &spyExecutor{Interface: kexec.New(), tracked: store}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	buildConnector := func(store *trackingNativeStore, callback *fakeCallback, enabled bool) managed.ExternalConnector {
		inner := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_subscription_database"], ujcontroller.WithCallbackProvider(callback))
		return procontroller.NewDeleteHoldConnector(kube, kube, inner, enabled)
	}
	connector := buildConnector(store, callback, os.Getenv("QA_PRODUCT_HOLD_TEST") == fixtureStringTrue)
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
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
	await("terraform apply ", mode == "create_unknown")
	if mode == "create_unknown" {
		verifyNoACLCreateReplay(t, rec, store, callback, name)
		mu.Lock()
		if !exists || unexpected || createRequests != 1 || deleteRequests != 0 {
			mu.Unlock()
			t.Fatal("uncertain Pro create was replayed, deleted or not retained")
		}
		mu.Unlock()
		for _, path := range []string{fixtureProParentPath, fixtureProDatabasePath} {
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
			_ = res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("fake read %s: %d", path, res.StatusCode)
			}
		}
		return
	}
	for i := 0; i < 5; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		current := &pro.Database{}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
			t.Fatal(err)
		}
		if current.Status.AtProvider.ID != nil && *current.Status.AtProvider.ID == fixtureProDBID {
			item = current
			break
		}
	}
	if item.Status.AtProvider.ID == nil || *item.Status.AtProvider.ID != fixtureProDBID {
		t.Fatal("Pro database identity missing")
	}
	updated := fixtureVolatileLRU
	item.Spec.ForProvider.DataEviction = &updated
	if err := kube.Update(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	verifyManagedDrift(t, rec, spy, callback, false)
	mu.Lock()
	if !exists || rule != updated || unexpected {
		mu.Unlock()
		t.Fatal("Pro database update or fixture trace invalid")
	}
	mu.Unlock()
	current := &pro.Database{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.AtProvider.ID == nil || *current.Status.AtProvider.ID != fixtureProDBID {
		t.Fatal("Pro database ID changed")
	}
	if err := kube.Delete(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	await("terraform destroy ", mode == "delete_unknown" || mode == "delete_delayed")
	if os.Getenv("QA_PRODUCT_HOLD_TEST") == fixtureStringTrue {
		// Reconstruct the managed reconciler and connector around the persisted CR.
		// This is not a process restart and proves only this fake Cloud fixture.
		store = newTrackingNativeStore()
		spy.tracked = store
		callback = &fakeCallback{done: make(chan error, 1)}
		connector = buildConnector(store, callback, false)
		rec = managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
		for i := 0; i < 3; i++ {
			if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
				t.Fatal(err)
			}
			retained := &pro.Database{}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, retained); err != nil || len(retained.Finalizers) == 0 {
				t.Fatal("held CR or finalizer lost")
			}
			if _, ok := retained.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"]; !ok {
				t.Fatal("intent lost")
			}
			if store.current != nil {
				t.Fatal("held reconcile opened an upstream workspace")
			}
		}
		mu.Lock()
		if unexpected || deleteRequests != 1 || (mode == "delete_delayed" && !exists) || (mode == fixtureCaseSuccess && exists) {
			mu.Unlock()
			t.Fatal("unexpected held fake state after initial reconciles")
		}
		// Model delayed fake disappearance while retaining the parent.
		exists = false
		mu.Unlock()
		for path, want := range map[string]int{fixtureProDatabasePath: http.StatusNotFound, fixtureProParentPath: http.StatusOK} {
			req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Api-Key", dummyCredential)
			req.Header.Set("X-Api-Secret-Key", dummyCredential)
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != want {
				t.Fatalf("fake read %s status %d", path, response.StatusCode)
			}
		}
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		if store.current != nil {
			t.Fatal("held reconcile opened an upstream workspace after fake absence")
		}
		held := &pro.Database{}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, held); err != nil || len(held.Finalizers) == 0 {
			t.Fatal("held finalizer lost after fake absence")
		}
		mu.Lock()
		if unexpected || deleteRequests != 1 {
			mu.Unlock()
			t.Fatal("delete replayed after fake absence")
		}
		mu.Unlock()
		return
	}
	if mode == "delete_unknown" {
		retained := &pro.Database{}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, retained); err != nil || len(retained.Finalizers) == 0 {
			t.Fatal("failed delete lost the managed CR or finalizer before observation")
		}
		for path, status := range map[string]int{fixtureProDatabasePath: http.StatusNotFound, fixtureProParentPath: http.StatusOK} {
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
			_ = res.Body.Close()
			if res.StatusCode != status {
				t.Fatalf("fake delete read %s: %d", path, res.StatusCode)
			}
		}
		mu.Lock()
		beforeDeleteReconcileReads = childAbsentReads
		mu.Unlock()
		verifyNoACLCreateReplay(t, rec, store, callback, name)
	}
	for i := 0; i < 30; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		if apierrors.IsNotFound(kube.Get(context.Background(), types.NamespacedName{Name: name}, &pro.Database{})) {
			break
		}
		if i == 29 {
			item := &pro.Database{}
			_ = kube.Get(context.Background(), types.NamespacedName{Name: name}, item)
			mu.Lock()
			t.Logf("fixture exists=%v unexpected=%v", exists, unexpected)
			mu.Unlock()
			t.Fatalf("Pro database CR remains: status=%#v finalizers=%v annotations=%v commands=%v", item.Status, item.Finalizers, item.Annotations, spy.snapshot())
		}
		time.Sleep(time.Second)
	}
	mu.Lock()
	if exists || unexpected || createRequests != 1 || deleteRequests != 1 || (mode == "delete_unknown" && childAbsentReads <= beforeDeleteReconcileReads) {
		mu.Unlock()
		t.Fatal("fake Pro database residue or unexpected request")
	}
	mu.Unlock()
	for path, status := range map[string]int{fixtureProDatabasePath: http.StatusNotFound, fixtureProParentPath: http.StatusOK} {
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
		_ = res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("fake read %s: %d", path, res.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "fixture-pro-db-uid")); !os.IsNotExist(err) {
		t.Fatal("workspace remains")
	}
}
