package qatfplan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
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

// TestProDatabaseManagedLifecycleLoopback uses actual reconciliation and Terraform
// against loopback fake Cloud, with a pre-existing fake parent; it never contacts Cloud.
const fixtureProDatabasePath = "/v1/subscriptions/1/databases/42"
const fixtureSourceCIDRMode = "source_cidrs"
const fixtureSourceCIDRDriftMode = "source_cidr_drift"
const fixtureInitialSourceCIDR = "192.0.2.0/24"

func TestProDatabaseManagedLifecycleLoopback(t *testing.T) {
	t.Run("lifecycle", func(t *testing.T) { runProDatabaseFixture(t, "success") })
	t.Run(fixtureSourceCIDRMode, func(t *testing.T) { runProDatabaseFixture(t, fixtureSourceCIDRMode) })
	t.Run(fixtureSourceCIDRDriftMode, func(t *testing.T) { runProDatabaseFixture(t, fixtureSourceCIDRDriftMode) })
	t.Run("accepted_then_503", func(t *testing.T) { runProDatabaseFixture(t, "create_unknown") })
	t.Run("delete_accepted_then_503", func(t *testing.T) { runProDatabaseFixture(t, "delete_unknown") })
}

func runProDatabaseFixture(t *testing.T, mode string) { //nolint:gocyclo // The fixture checks the complete ordered lifecycle.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	var mu sync.Mutex
	cidrMode := mode == fixtureSourceCIDRMode || mode == fixtureSourceCIDRDriftMode
	exists, unexpected := false, false
	createRequests, deleteRequests, childAbsentReads := 0, 0, 0
	childPresentReads := 0
	cidrUpdates := 0
	beforeDeleteReconcileReads := 0
	rule := desiredEviction
	sourceIPs := []any{}
	if cidrMode {
		sourceIPs = []any{fixtureInitialSourceCIDR}
	}
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
			if cidrMode && !reflect.DeepEqual(body["sourceIp"], sourceIPs) {
				unexpected = true
				http.Error(w, "incorrect source CIDRs", http.StatusBadRequest)
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
			childPresentReads++
			response = map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureEvictionKey: rule, fixtureStatusKey: fixtureActive, fixtureProtocolKey: fixtureProtocolValue, fixturePersistenceKey: fixtureNoPersistence, fixtureReplicationKey: false, "memoryLimitInGb": 1, "throughputMeasurement": map[string]any{"by": fixtureThroughputBy, "value": 1000}, fixtureSecurityKey: map[string]any{"enableDefaultUser": true, "enableTls": false, "tlsClientAuthentication": false, fixtureSourceIPsKey: sourceIPs}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{}, "clustering": map[string]any{"regexRules": []any{}}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureProDatabasePath && exists:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				unexpected = true
				http.Error(w, "malformed update", http.StatusBadRequest)
				return
			}
			if tracePath := os.Getenv("QA_SOURCE_PAYLOAD_TRACE"); tracePath != "" {
				sourceIP, present := body["sourceIp"]
				entry, err := json.Marshal(map[string]any{"present": present, "value": sourceIP})
				if err != nil {
					unexpected = true
					http.Error(w, "cannot encode fake source CIDRs", http.StatusInternalServerError)
					return
				}
				f, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					unexpected = true
					http.Error(w, "cannot open fake source trace", http.StatusInternalServerError)
					return
				}
				_, writeErr := f.Write(append(entry, '\n'))
				closeErr := f.Close()
				if writeErr != nil || closeErr != nil {
					unexpected = true
					http.Error(w, "cannot write fake source trace", http.StatusInternalServerError)
					return
				}
			}
			if cidrMode {
				expected := []any{fixtureInitialSourceCIDR}
				finalUpdate := 1
				if mode == fixtureSourceCIDRDriftMode {
					finalUpdate = 2
				}
				if cidrUpdates == finalUpdate {
					expected = []any{"198.51.100.0/24"}
				}
				if cidrUpdates > finalUpdate {
					unexpected = true
					http.Error(w, "unexpected additional CIDR update", http.StatusBadRequest)
					return
				}
				if !reflect.DeepEqual(body["sourceIp"], expected) {
					unexpected = true
					http.Error(w, "incorrect updated source CIDRs", http.StatusBadRequest)
					return
				}
				sourceIPs = expected
				cidrUpdates++
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
				rows = append(rows, map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureEvictionKey: rule, fixtureStatusKey: fixtureActive, fixtureProtocolKey: fixtureProtocolValue, fixturePersistenceKey: fixtureNoPersistence, fixtureReplicationKey: false, "memoryLimitInGb": 1, "throughputMeasurement": map[string]any{"by": fixtureThroughputBy, "value": 1000}, fixtureSecurityKey: map[string]any{"enableDefaultUser": true, "enableTls": false, "tlsClientAuthentication": false, fixtureSourceIPsKey: sourceIPs}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{}, "clustering": map[string]any{"regexRules": []any{}}})
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
	if cidrMode {
		cidr := fixtureInitialSourceCIDR
		item.Spec.ForProvider.SourceIps = []*string{&cidr}
	}
	parent, memory, by, throughput, persistence, replication := float64(1), float64(1), fixtureThroughputBy, float64(1000), fixtureNoPersistence, false
	item.Spec.ForProvider.SubscriptionID = &parent
	item.Spec.ForProvider.MemoryLimitInGb = &memory
	item.Spec.ForProvider.ThroughputMeasurementBy = &by
	item.Spec.ForProvider.ThroughputMeasurementValue = &throughput
	item.Spec.ForProvider.DataPersistence = &persistence
	item.Spec.ForProvider.Replication = &replication
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	store := newTrackingNativeStore()
	spy := &spyExecutor{Interface: kexec.New(), tracked: store}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_subscription_database"], ujcontroller.WithCallbackProvider(callback))
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
	markRawConfigPhase(t, "before_create", kube, name)
	await("terraform apply ", mode == "create_unknown")
	markRawConfigPhase(t, "after_create", kube, name)
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
	if cidrMode {
		observed := item.Status.AtProvider.SourceIps
		if len(observed) != 1 || observed[0] == nil || *observed[0] != fixtureInitialSourceCIDR {
			t.Fatal("initial source CIDR not observed")
		}
	}
	markRawConfigPhase(t, "after_observe", kube, name)
	if mode == fixtureSourceCIDRDriftMode {
		if store.current == nil || store.current.LastOperation.IsRunning() {
			t.Fatal("initial asynchronous operation has not settled")
		}
		select {
		case err := <-callback.done:
			t.Fatalf("unexpected prior callback before drift: %v", err)
		default:
		}
		mu.Lock()
		if cidrUpdates != 1 {
			mu.Unlock()
			t.Fatal("initial CIDR update was not observed")
		}
		beforeDriftRead := childPresentReads
		sourceIPs = []any{"0.0.0.0/0"}
		mu.Unlock()
		// Refresh and plan are synchronous Upjet workspace operations. Requiring
		// their successful results distinguishes masked drift from no observation.
		refresh, err := store.current.Refresh(context.Background())
		if err != nil || !refresh.Exists {
			t.Fatalf("drift refresh did not complete: %v", err)
		}
		plan, err := store.current.Plan(context.Background())
		if err != nil || !plan.Exists {
			t.Fatalf("drift plan did not complete: %v", err)
		}
		mu.Lock()
		observedDrift := childPresentReads > beforeDriftRead && cidrUpdates == 1 && reflect.DeepEqual(sourceIPs, []any{"0.0.0.0/0"}) && !unexpected
		mu.Unlock()
		if !observedDrift {
			t.Fatal("fake drift was not read without a mutation")
		}
		if plan.UpToDate {
			t.Fatal("observed CIDR drift produced an up-to-date plan")
		}
		// The desired CR remains unchanged; only the fake Cloud observation drifts.
		verifyManagedDrift(t, rec, spy, callback, false)
		mu.Lock()
		if childPresentReads <= beforeDriftRead || cidrUpdates != 2 || !reflect.DeepEqual(sourceIPs, []any{fixtureInitialSourceCIDR}) || unexpected {
			mu.Unlock()
			t.Fatal("observed source CIDR drift was not corrected")
		}
		mu.Unlock()
		if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, item); err != nil {
			t.Fatal(err)
		}
	}
	updated := fixtureVolatileLRU
	item.Spec.ForProvider.DataEviction = &updated
	if cidrMode {
		cidr := "198.51.100.0/24"
		item.Spec.ForProvider.SourceIps = []*string{&cidr}
	}
	if err := kube.Update(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	verifyManagedDrift(t, rec, spy, callback, false)
	markRawConfigPhase(t, "after_update", kube, name)
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
	if cidrMode {
		mu.Lock()
		readsBeforeObservation := childPresentReads
		wantCIDRUpdates := 2
		if mode == fixtureSourceCIDRDriftMode {
			wantCIDRUpdates = 3
		}
		if cidrUpdates != wantCIDRUpdates {
			mu.Unlock()
			t.Fatal("initial and changed source CIDR updates were not sent")
		}
		mu.Unlock()
		for i := 0; i < 5; i++ {
			if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
				t.Fatal(err)
			}
			if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
				t.Fatal(err)
			}
			observed := current.Status.AtProvider.SourceIps
			mu.Lock()
			freshRead := childPresentReads > readsBeforeObservation
			mu.Unlock()
			if freshRead && len(observed) == 1 && observed[0] != nil && *observed[0] == "198.51.100.0/24" {
				break
			}
			if i == 4 {
				t.Fatal("updated source CIDR not observed")
			}
		}
	}
	if err := kube.Delete(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	await("terraform destroy ", mode == "delete_unknown")
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
