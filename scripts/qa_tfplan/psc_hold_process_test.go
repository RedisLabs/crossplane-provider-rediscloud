package qatfplan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	pro "github.com/RedisLabs/provider-rediscloud/apis/private/v1beta1"
	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	procontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/private/serviceconnectendpoint"
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
	pscProcessFirst = "first"
	pscProcessHTTP  = "http"
)

// The supervisor retains a loopback fake API and API server while two distinct
// controller test processes run sequentially. It is not a Kubernetes pod test.
func TestProductPSCHoldProcessRestart(t *testing.T) { //nolint:gocyclo // Ordered process and resource receipts are all required.
	runProductPSCHoldProcesses(t, false)
}

func TestProductPSCHoldAbruptKill(t *testing.T) {
	runProductPSCHoldProcesses(t, true)
}

func runProductPSCHoldProcesses(t *testing.T, abrupt bool) { //nolint:gocyclo // Ordered process and resource receipts are all required.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Fatal("pinned provider and Terraform required")
	}
	var mu sync.Mutex
	accepted := make(chan struct{}, 1)
	releaseResponse := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseResponse) }) }
	// On every failure, unblock the handler before httptest.Server closes.
	defer release()
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
			result = map[string]any{"id": 1, fixtureNameKey: "fixture-parent", fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/private-service-connect":
			result = map[string]any{fixtureTaskKey: pscServiceTask}
		case r.Method == http.MethodGet && r.URL.Path == pscServiceTaskPath:
			result = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureResourceKey: map[string]any{"id": 7, fixtureStatusKey: fixtureActive}}}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7":
			result = map[string]any{fixtureTaskKey: pscEndpointsTask}
		case r.Method == http.MethodGet && r.URL.Path == pscEndpointsTaskPath:
			reads++
			endpoints := []any{}
			if exists {
				endpoints = append(endpoints, map[string]any{"id": 8, "gcpProjectId": pscFixtureProject, "gcpVpcName": pscFixtureVPC, "gcpVpcSubnetName": pscFixtureSubnet, "endpointConnectionName": pscFixturePrefix, fixtureStatusKey: pscEndpointInitialized})
			}
			result = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureResourceKey: map[string]any{"pscServiceId": 7, "endpoints": endpoints}}}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7/endpoints/8/creationScripts" && exists:
			result = map[string]any{fixtureTaskKey: "get-script"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-script" && exists:
			result = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureResourceKey: map[string]any{"script": map[string]any{"terraformGcp": map[string]any{"serviceAttachments": []any{}}}}}}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7":
			creates++
			var body map[string]any
			if exists || creates != 1 || json.NewDecoder(r.Body).Decode(&body) != nil || body["gcpProjectId"] != pscFixtureProject || body["gcpVpcName"] != pscFixtureVPC || body["gcpVpcSubnetName"] != pscFixtureSubnet || body["endpointConnectionName"] != pscFixturePrefix {
				unexpected = true
				http.Error(w, "bad fake create", http.StatusBadRequest)
				return
			}
			exists = true
			result = map[string]any{fixtureTaskKey: "create-endpoint"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/create-endpoint" && exists:
			result = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{"resourceId": 8}}
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7/endpoints/8":
			deletes++
			if deletes != 1 || !exists {
				unexpected = true
			}
			if abrupt && deletes == 1 {
				accepted <- struct{}{}
				<-releaseResponse
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
	defer func() { release(); server.Close() }()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("QA_PSC_PROCESS_CONFIG", filepath.Join(dir, "api-config.json"))
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
	item := &pro.ServiceConnectEndpoint{ObjectMeta: metav1.ObjectMeta{Name: fixtureName, Finalizers: []string{managed.FinalizerName}}}
	sub, service := "1", float64(7)
	project, vpc, subnet, prefix := pscFixtureProject, pscFixtureVPC, pscFixtureSubnet, pscFixturePrefix
	item.Spec.ForProvider.SubscriptionID, item.Spec.ForProvider.PrivateServiceConnectServiceID = &sub, &service
	item.Spec.ForProvider.GCPProjectID, item.Spec.ForProvider.GCPVPCName = &project, &vpc
	item.Spec.ForProvider.GCPVPCSubnetName, item.Spec.ForProvider.EndpointConnectionName = &subnet, &prefix
	item.Spec.ProviderConfigReference = &xpv2.Reference{Name: pscTestProviderConfig}
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := newPSCEnvtestClient(t, scheme, item)
	uid := item.UID
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pids := make([]int, 0, 2)
	var originalIntent string
	for _, role := range []string{pscProcessFirst, "second"} {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestProductPSCHoldProcessWorker$", "-test.v")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.WaitDelay = 30 * time.Second
		cmd.Env = append(os.Environ(), "QA_RESTART_WORKER="+role)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if startErr := cmd.Start(); startErr != nil {
			cancel()
			t.Fatal(startErr)
		}
		pid := cmd.Process.Pid
		if abrupt && role == pscProcessFirst {
			finished := make(chan error, 1)
			go func() { finished <- cmd.Wait() }()
			select {
			case <-accepted:
				if killErr := syscall.Kill(-pid, syscall.SIGKILL); killErr != nil {
					release()
					cancel()
					t.Fatalf("cannot kill worker group: %v", killErr)
				}
				select {
				case err = <-finished:
				case <-time.After(30 * time.Second):
					release()
					cancel()
					t.Fatal("worker process group did not terminate")
				}
				waitStatus, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
				if err == nil || !ok || !waitStatus.Signaled() || waitStatus.Signal() != syscall.SIGKILL {
					release()
					cancel()
					t.Fatalf("worker did not exit via SIGKILL: %v", err)
				}
				if !waitPSCProcessGroupGone(pid) {
					release()
					cancel()
					t.Fatal("worker process group still exists")
				}
				if strings.Contains(output.String(), "PROCESS_RECEIPT first ") {
					release()
					cancel()
					t.Fatal("first worker reported callback before kill")
				}
				release()
			case err = <-finished:
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				release()
				cancel()
				t.Fatalf("worker exited before accepted fake delete: %v; %s", err, output.String())
			case <-ctx.Done():
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				release()
				select {
				case waitErr := <-finished:
					_ = waitErr // Timeout path is already a test failure.
				case <-time.After(30 * time.Second):
				}
				cancel()
				t.Fatal("fake delete did not arrive")
			}
			cancel()
		} else {
			err = cmd.Wait()
			cancel()
			if err != nil {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				mu.Lock()
				paths := append([]string(nil), trace...)
				mu.Unlock()
				t.Fatalf("%s worker failed: %v; fake paths=%v\n%s", role, err, paths, output.String())
			}
			if !strings.Contains(output.String(), fmt.Sprintf("PROCESS_RECEIPT %s %d", role, pid)) {
				t.Fatalf("%s worker receipt missing", role)
			}
		}
		pids = append(pids, pid)
		current := &pro.ServiceConnectEndpoint{}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: item.Name}, current); err != nil || current.UID != uid || current.GetDeletionTimestamp().IsZero() || !slices.Contains(current.Finalizers, managed.FinalizerName) {
			t.Fatal("persisted CR/finalizer missing")
		}
		var marker struct {
			UID, External, Provider, Token string
			Version                        int
		}
		intent := current.Annotations[pscTestIntentAnnotation]
		if json.Unmarshal([]byte(intent), &marker) != nil || marker.Version != 1 || marker.UID != string(uid) || marker.External != pscTestEndpointExternal || marker.Provider != pscTestProviderConfig || marker.Token == "" {
			t.Fatal("PSC intent identity missing")
		}
		if role == pscProcessFirst {
			originalIntent = intent
		} else if intent != originalIntent {
			t.Fatal("PSC intent changed across worker processes")
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
func TestProductPSCHoldProcessWorker(t *testing.T) { //nolint:gocyclo // Worker validates the complete process boundary and outcome.
	role := os.Getenv("QA_RESTART_WORKER")
	if role == "" {
		t.Skip("only launched by hosted supervisor")
	}
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue || (role != pscProcessFirst && role != "second") {
		t.Fatal("invalid worker context")
	}
	url := os.Getenv("QA_RESTART_FIXTURE_URL")
	if !validPSCProcessLoopback(url, pscProcessHTTP) {
		t.Fatal("fixture must be loopback")
	}
	data, err := os.ReadFile(os.Getenv("QA_PSC_PROCESS_CONFIG"))
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
	if !validPSCProcessLoopback(saved.Host, "https") {
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
	callback := &pscProcessCallbacks{done: make(chan pscProcessCallbackEvent, 8)}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: url + "/v1"}}, nil
	}
	inner := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_private_service_connect_endpoint"], ujcontroller.WithCallbackProvider(callback))
	connector := procontroller.NewDeleteHoldConnector(kube, kube, inner, role == pscProcessFirst)
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.ServiceConnectEndpoint_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}
	if role == pscProcessFirst {
		waitOperation := func(wantKind string, expectFailure bool) {
			t.Helper()
			for i := 0; i < 8; i++ {
				if _, reconcileErr := rec.Reconcile(context.Background(), req); reconcileErr != nil {
					t.Fatal(reconcileErr)
				}
				select {
				case event := <-callback.done:
					matched, checkErr := checkPSCProcessCallback(event, wantKind, expectFailure)
					if checkErr != nil {
						t.Fatal(checkErr)
					}
					if matched {
						return
					}
				case <-time.After(45 * time.Second):
				}
			}
			current := &pro.ServiceConnectEndpoint{}
			if getErr := kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); getErr != nil {
				t.Fatal("operation callback and CR missing")
			}
			t.Fatalf("operation callback missing; fake CR conditions=%+v", current.Status.Conditions)
		}
		waitOperation("create", false)
		var current *pro.ServiceConnectEndpoint
		for i := 0; i < 6; i++ {
			if _, reconcileErr := rec.Reconcile(context.Background(), req); reconcileErr != nil {
				t.Fatal(reconcileErr)
			}
			current = &pro.ServiceConnectEndpoint{}
			if getErr := kube.Get(context.Background(), types.NamespacedName{Name: fixtureName}, current); getErr != nil {
				t.Fatal(getErr)
			}
			if current.Status.AtProvider.ID != nil && *current.Status.AtProvider.ID == pscTestEndpointExternal && meta.GetExternalName(current) == pscTestEndpointExternal {
				break
			}
		}
		if current == nil || current.Status.AtProvider.ID == nil || *current.Status.AtProvider.ID != pscTestEndpointExternal || meta.GetExternalName(current) != pscTestEndpointExternal {
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
func validPSCProcessLoopback(raw, scheme string) bool { //nolint:gocyclo // Each origin boundary is checked explicitly.
	parsed, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "?#") || parsed.Scheme != scheme || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.User != nil || (parsed.Path != "" && (scheme != "https" || parsed.Path != "/")) {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port >= 1 && port <= 65535
}

func TestProductPSCProcessLoopbackValidation(t *testing.T) {
	for _, tc := range []struct {
		raw, scheme string
		want        bool
	}{
		{"http://127.0.0.1:1234", pscProcessHTTP, true},
		{"http://127.0.0.1:1234/", pscProcessHTTP, false},
		{"https://127.0.0.1:1234", "https", true},
		{"https://127.0.0.1:1234/", "https", true},
		{"http://127.0.0.1:1@other-host:1234", pscProcessHTTP, false},
		{"http://127.0.0.1:1234/path", pscProcessHTTP, false},
		{"http://127.0.0.1:1234?", pscProcessHTTP, false},
		{"http://127.0.0.1:99999", pscProcessHTTP, false},
		{"https://127.0.0.1:1234", pscProcessHTTP, false},
	} {
		if got := validPSCProcessLoopback(tc.raw, tc.scheme); got != tc.want {
			t.Fatalf("unexpected origin validation: %q", tc.raw)
		}
	}
}

// Kind-specific receipts prevent a late update callback masquerading as delete.
type pscProcessCallbackEvent struct {
	kind string
	err  error
}
type pscProcessCallbacks struct{ done chan pscProcessCallbackEvent }

func (c *pscProcessCallbacks) result(kind string) terraform.CallbackFn {
	return func(err error, _ context.Context) error {
		c.done <- pscProcessCallbackEvent{kind: kind, err: err}
		return nil
	}
}
func (c *pscProcessCallbacks) Create(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("create")
}
func (c *pscProcessCallbacks) Update(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("update")
}
func (c *pscProcessCallbacks) Destroy(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("destroy")
}
func checkPSCProcessCallback(event pscProcessCallbackEvent, want string, failure bool) (bool, error) {
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
func TestProductPSCProcessCallbackAttribution(t *testing.T) {
	fakeError := fmt.Errorf("fake failure")
	if matched, err := checkPSCProcessCallback(pscProcessCallbackEvent{kind: "update", err: fakeError}, "destroy", true); matched || err == nil {
		t.Fatal("failed update mistaken for destroy")
	}
	if matched, err := checkPSCProcessCallback(pscProcessCallbackEvent{kind: "update"}, "destroy", true); matched || err != nil {
		t.Fatal("successful update should not satisfy destroy")
	}
	if matched, err := checkPSCProcessCallback(pscProcessCallbackEvent{kind: "destroy", err: fakeError}, "destroy", true); !matched || err != nil {
		t.Fatal("expected failed destroy missing")
	}
}

// Ensure process-group descendants have exited before the held response is released.
func waitPSCProcessGroupGone(pid int) bool {
	for i := 0; i < 50; i++ {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if err != nil {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
