package qatfplan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	procontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/subscription/database"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	ujcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	fixtureFirst = "first"
	fixtureHTTP  = "http"
)

// The supervisor retains a loopback fake API and API server while two distinct
// controller test processes run sequentially. It is not a Kubernetes pod test.
func TestProductProHoldProcessRestart(t *testing.T) { //nolint:gocyclo // Ordered process and resource receipts are all required.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Fatal("pinned provider and Terraform required")
	}
	var mu sync.Mutex
	deletes, reads, creates := 0, 0, 0
	exists := false
	var trace []string
	unexpected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			unexpected = true
			http.Error(w, "unauthorized fixture", http.StatusForbidden)
			return
		}
		trace = append(trace, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == fixtureProParentPath:
			result = map[string]any{"id": 1, fixtureNameKey: "fixture-existing-parent", fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodPost && r.URL.Path == fixtureProDBsPath:
			creates++
			if exists || creates != 1 {
				unexpected = true
				http.Error(w, "duplicate create", http.StatusConflict)
				return
			}
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body[fixtureNameKey] != fixtureProDBName || body[fixtureEvictionKey] != desiredEviction {
				unexpected = true
				http.Error(w, "bad create", http.StatusBadRequest)
				return
			}
			exists = true
			result = map[string]any{fixtureTaskKey: fixtureCreateTask}
		case r.Method == http.MethodGet && r.URL.Path == fixtureCreateTaskPath:
			result = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureTaskResourceKey: 42}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDatabasePath && exists:
			reads++
			result = map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureEvictionKey: desiredEviction, fixtureStatusKey: fixtureActive, fixtureProtocolKey: fixtureProtocolValue, fixturePersistenceKey: fixtureNoPersistence, fixtureReplicationKey: false, "memoryLimitInGb": 1, "throughputMeasurement": map[string]any{"by": fixtureThroughputBy, "value": 1000}, fixtureSecurityKey: map[string]any{"enableDefaultUser": true, "enableTls": false, "tlsClientAuthentication": false, fixtureSourceIPsKey: []any{}}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{}, "clustering": map[string]any{"regexRules": []any{}}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDatabasePath+"/tags":
			result = map[string]any{fixtureTagsKey: []any{}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureProDatabasePath && exists:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				unexpected = true
				http.Error(w, "bad update", http.StatusBadRequest)
				return
			}
			if policy, ok := body[fixtureEvictionKey]; ok && policy != desiredEviction {
				unexpected = true
				http.Error(w, "unexpected policy", http.StatusBadRequest)
				return
			}
			result = map[string]any{fixtureTaskKey: fixtureUpdateTask}
		case r.Method == http.MethodGet && r.URL.Path == fixtureUpdateTaskPath:
			result = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodPut && r.URL.Path == fixtureProDatabasePath+"/tags":
			result = map[string]any{fixtureTagsKey: []any{}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDBsPath:
			rows := []any{}
			if exists {
				rows = append(rows, map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureStatusKey: fixtureActive})
			}
			result = map[string]any{fixtureDatabasesKey: rows}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureProDatabasePath:
			deletes++
			if deletes != 1 || !exists {
				unexpected = true
			}
			http.Error(w, "accepted with lost response", http.StatusServiceUnavailable)
			return
		default:
			unexpected = true
			http.Error(w, "unexpected fixture request", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("TMPDIR", dir)
	t.Setenv("QA_RESTART_CONFIG_OUT", filepath.Join(dir, "api-config.json"))
	t.Setenv("QA_RESTART_FIXTURE_URL", server.URL)
	t.Setenv("TF_CLI_CONFIG_FILE", filepath.Join(dir, "offline.tfrc"))
	if err := os.WriteFile(filepath.Join(dir, "offline.tfrc"), []byte(fmt.Sprintf("provider_installation {\n filesystem_mirror { path = %q }\n}\n", plugins)), 0o600); err != nil {
		t.Fatal(err)
	}
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
	scheme := runtime.NewScheme()
	if err := pro.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	item := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: fixtureName, Finalizers: []string{managed.FinalizerName}}}
	parent, memory, throughput := float64(1), float64(1), float64(1000)
	name, eviction, by, persistence, replication := fixtureProDBName, desiredEviction, fixtureThroughputBy, fixtureNoPersistence, false
	item.Spec.ForProvider.Name = &name
	item.Spec.ForProvider.SubscriptionID = &parent
	item.Spec.ForProvider.DataEviction = &eviction
	item.Spec.ForProvider.MemoryLimitInGb = &memory
	item.Spec.ForProvider.ThroughputMeasurementBy = &by
	item.Spec.ForProvider.ThroughputMeasurementValue = &throughput
	item.Spec.ForProvider.DataPersistence = &persistence
	item.Spec.ForProvider.Replication = &replication
	item.Spec.ProviderConfigReference = &xpv2.Reference{Name: "fixture-config"}
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := newProEnvtestClient(t, scheme, item)
	uid := item.UID
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pids := make([]int, 0, 2)
	for _, role := range []string{fixtureFirst, "second"} {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestProductProHoldProcessWorker$", "-test.v")
		cmd.Env = append(os.Environ(), "QA_RESTART_WORKER="+role)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if startErr := cmd.Start(); startErr != nil {
			cancel()
			t.Fatal(startErr)
		}
		pid := cmd.Process.Pid
		err = cmd.Wait()
		cancel()
		if err != nil {
			mu.Lock()
			paths := append([]string(nil), trace...)
			mu.Unlock()
			t.Fatalf("%s worker failed: %v; fake paths=%v\n%s", role, err, paths, output.String())
		}
		if !strings.Contains(output.String(), fmt.Sprintf("PROCESS_RECEIPT %s %d", role, pid)) {
			t.Fatalf("%s worker receipt missing", role)
		}
		pids = append(pids, pid)
		current := &pro.Database{}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: item.Name}, current); err != nil || current.UID != uid || len(current.Finalizers) == 0 {
			t.Fatal("persisted CR/finalizer missing")
		}
		if _, ok := current.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"]; !ok {
			t.Fatal("intent missing")
		}
		mu.Lock()
		if deletes != 1 || creates != 1 || reads == 0 || !exists || unexpected {
			mu.Unlock()
			t.Fatal("fake request trace invalid")
		}
		mu.Unlock()
	}
	if len(pids) != 2 || pids[0] == pids[1] || pids[0] == os.Getpid() || pids[1] == os.Getpid() {
		t.Fatal("workers were not distinct processes")
	}
	if _, err := os.Stat(filepath.Join(dir, string(uid))); err != nil {
		t.Fatal("workspace not retained")
	}
}

// This worker only talks to a private envtest server and loopback fake API.
func TestProductProHoldProcessWorker(t *testing.T) { //nolint:gocyclo // Worker validates the complete process boundary and outcome.
	role := os.Getenv("QA_RESTART_WORKER")
	if role == "" {
		t.Skip("only launched by hosted supervisor")
	}
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue || (role != fixtureFirst && role != "second") {
		t.Fatal("invalid worker context")
	}
	url := os.Getenv("QA_RESTART_FIXTURE_URL")
	if !validProcessLoopback(url, fixtureHTTP) {
		t.Fatal("fixture must be loopback")
	}
	data, err := os.ReadFile(os.Getenv("QA_RESTART_CONFIG_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	saved := struct {
		Host            string
		TLSClientConfig rest.TLSClientConfig
	}{}
	if decodeErr := json.Unmarshal(data, &saved); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if !validProcessLoopback(saved.Host, "https") {
		t.Fatal("API server must be loopback")
	}
	cfg := &rest.Config{Host: saved.Host, TLSClientConfig: saved.TLSClientConfig}
	scheme := runtime.NewScheme()
	if schemeErr := pro.AddToScheme(scheme); schemeErr != nil {
		t.Fatal(schemeErr)
	}
	kube, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	store := newTrackingNativeStore()
	callback := &processCallbacks{done: make(chan processCallbackEvent, 8)}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: url + "/v1"}}, nil
	}
	inner := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_subscription_database"], ujcontroller.WithCallbackProvider(callback))
	connector := procontroller.NewDeleteHoldConnector(kube, kube, inner, role == fixtureFirst)
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}
	if role == fixtureFirst {
		waitOperation := func(wantKind string, expectFailure bool) {
			t.Helper()
			for i := 0; i < 8; i++ {
				if _, reconcileErr := rec.Reconcile(context.Background(), req); reconcileErr != nil {
					t.Fatal(reconcileErr)
				}
				select {
				case event := <-callback.done:
					matched, checkErr := checkProcessCallback(event, wantKind, expectFailure)
					if checkErr != nil {
						t.Fatal(checkErr)
					}
					if matched {
						return
					}
				case <-time.After(45 * time.Second):
				}
			}
			current := &pro.Database{}
			if getErr := kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); getErr != nil {
				t.Fatal("operation callback and CR missing")
			}
			t.Fatalf("operation callback missing; fake CR conditions=%+v", current.Status.Conditions)
		}
		waitOperation("create", false)
		var current *pro.Database
		for i := 0; i < 6; i++ {
			if _, reconcileErr := rec.Reconcile(context.Background(), req); reconcileErr != nil {
				t.Fatal(reconcileErr)
			}
			current = &pro.Database{}
			if getErr := kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); getErr != nil {
				t.Fatal(getErr)
			}
			if current.Status.AtProvider.ID != nil && *current.Status.AtProvider.ID == fixtureProDBID && meta.GetExternalName(current) == fixtureProDBID {
				break
			}
		}
		if current == nil || current.Status.AtProvider.ID == nil || *current.Status.AtProvider.ID != fixtureProDBID || meta.GetExternalName(current) != fixtureProDBID {
			t.Fatal("created fake identity missing")
		}
		if deleteErr := kube.Delete(context.Background(), current); deleteErr != nil {
			t.Fatal(deleteErr)
		}
		waitOperation("destroy", true)
		fmt.Printf("PROCESS_RECEIPT %s %d\n", role, os.Getpid())
		return
	}
	for i := 0; i < 6; i++ {
		if _, reconcileErr := rec.Reconcile(context.Background(), req); reconcileErr != nil {
			t.Fatal(reconcileErr)
		}
		if store.current != nil {
			t.Fatal("held worker opened workspace")
		}
	}
	fmt.Printf("PROCESS_RECEIPT %s %s\n", role, strconv.Itoa(os.Getpid()))
}

// The worker cannot target an arbitrary origin even if its environment is altered.
func validProcessLoopback(raw, scheme string) bool { //nolint:gocyclo // Each origin boundary is checked explicitly.
	parsed, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "?#") || parsed.Scheme != scheme || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.User != nil || (parsed.Path != "" && (scheme != "https" || parsed.Path != "/")) {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port >= 1 && port <= 65535
}

func TestProductProcessLoopbackValidation(t *testing.T) {
	for _, tc := range []struct {
		raw, scheme string
		want        bool
	}{
		{"http://127.0.0.1:1234", fixtureHTTP, true},
		{"http://127.0.0.1:1234/", fixtureHTTP, false},
		{"https://127.0.0.1:1234", "https", true},
		{"https://127.0.0.1:1234/", "https", true},
		{"http://127.0.0.1:1@other-host:1234", fixtureHTTP, false},
		{"http://127.0.0.1:1234/path", fixtureHTTP, false},
		{"http://127.0.0.1:1234?", fixtureHTTP, false},
		{"http://127.0.0.1:99999", fixtureHTTP, false},
		{"https://127.0.0.1:1234", fixtureHTTP, false},
	} {
		if got := validProcessLoopback(tc.raw, tc.scheme); got != tc.want {
			t.Fatalf("unexpected origin validation: %q", tc.raw)
		}
	}
}

// Kind-specific receipts prevent a late update callback masquerading as delete.
type processCallbackEvent struct {
	kind string
	err  error
}
type processCallbacks struct{ done chan processCallbackEvent }

func (c *processCallbacks) result(kind string) terraform.CallbackFn {
	return func(err error, _ context.Context) error {
		c.done <- processCallbackEvent{kind: kind, err: err}
		return nil
	}
}
func (c *processCallbacks) Create(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("create")
}
func (c *processCallbacks) Update(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("update")
}
func (c *processCallbacks) Destroy(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("destroy")
}
func checkProcessCallback(event processCallbackEvent, want string, failure bool) (bool, error) {
	if event.kind == "update" && want == "destroy" {
		if event.err != nil {
			return false, fmt.Errorf("initial fake update failed")
		}
		return false, nil
	}
	if event.kind != want || (event.err != nil) != failure {
		return false, fmt.Errorf("unexpected callback kind or outcome")
	}
	return true, nil
}
func TestProductProcessCallbackAttribution(t *testing.T) {
	fakeError := fmt.Errorf("fake failure")
	if matched, err := checkProcessCallback(processCallbackEvent{kind: "update", err: fakeError}, "destroy", true); matched || err == nil {
		t.Fatal("failed update mistaken for destroy")
	}
	if matched, err := checkProcessCallback(processCallbackEvent{kind: "update"}, "destroy", true); matched || err != nil {
		t.Fatal("successful update should not satisfy destroy")
	}
	if matched, err := checkProcessCallback(processCallbackEvent{kind: "destroy", err: fakeError}, "destroy", true); !matched || err != nil {
		t.Fatal("expected failed destroy missing")
	}
}
