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

// The supervisor retains a loopback fake API and API server while two distinct
// controller test processes run sequentially. It is not a Kubernetes pod test.
func TestProductProHoldProcessRestart(t *testing.T) { //nolint:gocyclo // Ordered process and resource receipts are all required.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Fatal("pinned provider and Terraform required")
	}
	var mu sync.Mutex
	deletes, reads := 0, 0
	unexpected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			unexpected = true
			http.Error(w, "unauthorized fixture", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == fixtureProParentPath:
			result = map[string]any{"id": 1, fixtureNameKey: "fixture-existing-parent", fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDatabasePath:
			reads++
			result = map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureEvictionKey: desiredEviction, fixtureStatusKey: fixtureActive, fixtureProtocolKey: fixtureProtocolValue, fixturePersistenceKey: fixtureNoPersistence, fixtureReplicationKey: false, "memoryLimitInGb": 1, "throughputMeasurement": map[string]any{"by": fixtureThroughputBy, "value": 1000}, fixtureSecurityKey: map[string]any{"enableDefaultUser": true, "enableTls": false, "tlsClientAuthentication": false, fixtureSourceIPsKey: []any{}}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{}, "clustering": map[string]any{"regexRules": []any{}}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDatabasePath+"/tags":
			result = map[string]any{fixtureTagsKey: []any{}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDBsPath:
			result = map[string]any{fixtureDatabasesKey: []any{map[string]any{fixtureDatabaseIDKey: 42, fixtureNameKey: fixtureProDBName, fixtureStatusKey: fixtureActive}}}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureProDatabasePath:
			deletes++
			if deletes != 1 {
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
	meta.SetExternalName(item, fixtureProDBID)
	kube := newProEnvtestClient(t, scheme, item)
	uid := item.UID
	if err := kube.Delete(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, role := range []string{"first", "second"} {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestProductProHoldProcessWorker$", "-test.v")
		cmd.Env = append(os.Environ(), "QA_RESTART_WORKER="+role)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		pid := cmd.Process.Pid
		err = cmd.Wait()
		cancel()
		if err != nil {
			t.Fatalf("%s worker failed: %v\n%s", role, err, output.String())
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
		if deletes != 1 || reads == 0 || unexpected {
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
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue || (role != "first" && role != "second") {
		t.Fatal("invalid worker context")
	}
	url := os.Getenv("QA_RESTART_FIXTURE_URL")
	if !validProcessLoopback(url, "http") {
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
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !validProcessLoopback(saved.Host, "https") {
		t.Fatal("API server must be loopback")
	}
	cfg := &rest.Config{Host: saved.Host, TLSClientConfig: saved.TLSClientConfig}
	scheme := runtime.NewScheme()
	if err := pro.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kube, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	store := newTrackingNativeStore()
	callback := &fakeCallback{done: make(chan error, 1)}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: url + "/v1"}}, nil
	}
	inner := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_subscription_database"], ujcontroller.WithCallbackProvider(callback))
	connector := procontroller.NewDeleteHoldConnector(kube, kube, inner, role == "first")
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}
	for i := 0; i < 6; i++ {
		if _, err := rec.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if role == "second" {
			if store.current != nil {
				t.Fatal("held worker opened workspace")
			}
			continue
		}
		select {
		case err := <-callback.done:
			if err == nil {
				t.Fatal("expected failed delete callback")
			}
			fmt.Printf("PROCESS_RECEIPT %s %d\n", role, os.Getpid())
			return
		case <-time.After(45 * time.Second):
		}
	}
	if role == "first" {
		t.Fatal("failed delete callback missing")
	}
	fmt.Printf("PROCESS_RECEIPT %s %s\n", role, strconv.Itoa(os.Getpid()))
}

// The worker cannot target an arbitrary origin even if its environment is altered.
func validProcessLoopback(raw, scheme string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "?#") || parsed.Scheme != scheme || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.User != nil || parsed.Path != "" {
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
		{"http://127.0.0.1:1234", "http", true},
		{"https://127.0.0.1:1234", "https", true},
		{"http://127.0.0.1:1@other-host:1234", "http", false},
		{"http://127.0.0.1:1234/path", "http", false},
		{"http://127.0.0.1:1234?", "http", false},
		{"http://127.0.0.1:99999", "http", false},
		{"https://127.0.0.1:1234", "http", false},
	} {
		if got := validProcessLoopback(tc.raw, tc.scheme); got != tc.want {
			t.Fatalf("unexpected origin validation: %q", tc.raw)
		}
	}
}
