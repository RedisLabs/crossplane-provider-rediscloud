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

	pro "github.com/RedisLabs/provider-rediscloud/apis/rediscloud/v1beta1"
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

// This fixture uses no Cloud account: it models a newly created Pro parent and
// one bootstrap database on loopback, and checks that both are removed.
const (
	fixtureProParentName     = "fixture-pro-parent"
	fixtureBootstrapPath     = "/v1/subscriptions/1/databases/2"
	fixtureSubscriptionsPath = "/v1/subscriptions"
)

func TestProSubscriptionManagedLifecycleLoopback(t *testing.T) {
	t.Run("gcp", func(t *testing.T) { runProSubscriptionFixture(t, fixtureGCP, fixtureRegion, false) })
	t.Run("aws", func(t *testing.T) { runProSubscriptionFixture(t, "AWS", "us-east-1", false) })
	t.Run("accepted_then_503", func(t *testing.T) { runProSubscriptionFixture(t, fixtureGCP, fixtureRegion, true) })
}

func runProSubscriptionFixture(t *testing.T, cloud, region string, acceptedUnknown bool) { //nolint:gocyclo // Ordered fake lifecycle is deliberately kept together.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	var mu sync.Mutex
	parent, bootstrap, bootstrapRemoved, unexpected := false, false, false, false
	createRequests, deleteRequests := 0, 0
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodDelete {
			deleteRequests++
		}
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			unexpected = true
			http.Error(w, "fixture credentials required", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == fixtureSubscriptionsPath:
			rows := []any{}
			if parent {
				rows = append(rows, map[string]any{"id": 1, fixtureNameKey: fixtureProParentName})
			}
			response = map[string]any{"subscriptions": rows}
		case r.Method == http.MethodPost && r.URL.Path == fixtureSubscriptionsPath:
			createRequests++
			if parent {
				unexpected = true
				http.Error(w, "duplicate parent create", http.StatusConflict)
				return
			}
			var body struct {
				Name           string `json:"name"`
				CloudProviders []struct {
					Provider string `json:"provider"`
					Regions  []struct {
						Region string `json:"region"`
					} `json:"regions"`
				} `json:"cloudProviders"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Name != fixtureProParentName || len(body.CloudProviders) != 1 || body.CloudProviders[0].Provider != cloud || len(body.CloudProviders[0].Regions) != 1 || body.CloudProviders[0].Regions[0].Region != region {
				unexpected = true
				http.Error(w, "bad parent create", http.StatusBadRequest)
				return
			}
			parent, bootstrap = true, true
			if acceptedUnknown {
				http.Error(w, "injected accepted create with unavailable response", http.StatusServiceUnavailable)
				return
			}
			response = map[string]any{fixtureTaskKey: "parent-create"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/parent-create":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureTaskResourceKey: 1}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProParentPath && parent:
			response = map[string]any{"id": 1, fixtureNameKey: fixtureProParentName, fixtureStatusKey: fixtureActive, "paymentMethodType": fixturePaymentType, "paymentMethodId": 3, "memoryStorage": "ram", "publicEndpointAccess": true, "cloudDetails": []any{map[string]any{"provider": cloud, "cloudAccountId": 1, "regions": []any{map[string]any{"region": region, "multipleAvailabilityZones": false, "networking": []any{map[string]any{"deploymentCIDR": "10.0.0.0/24"}}}}}}}
		case r.Method == http.MethodPut && r.URL.Path == fixtureProParentPath && parent:
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || (body[fixtureNameKey] != nil && body[fixtureNameKey] != fixtureProParentName) {
				unexpected = true
				http.Error(w, "bad parent update", http.StatusBadRequest)
				return
			}
			response = map[string]any{fixtureTaskKey: "parent-update"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/parent-update":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProDBsPath:
			dbs := []any{}
			if bootstrap {
				dbs = append(dbs, map[string]any{fixtureDatabaseIDKey: 2, fixtureNameKey: "fixture-bootstrap", fixtureStatusKey: fixtureActive})
			}
			response = map[string]any{"subscription": []any{map[string]any{fixtureSubscriptionIDKey: 1, fixtureDatabasesKey: dbs}}}
		case r.Method == http.MethodGet && r.URL.Path == fixtureBootstrapPath && bootstrap:
			response = map[string]any{fixtureDatabaseIDKey: 2, fixtureNameKey: "fixture-bootstrap", fixtureStatusKey: fixtureActive}
		case r.Method == http.MethodGet && r.URL.Path == fixtureBootstrapPath && !bootstrap:
			http.NotFound(w, r)
			return
		case r.Method == http.MethodDelete && r.URL.Path == fixtureBootstrapPath && bootstrap:
			bootstrap = false
			bootstrapRemoved = true
			response = map[string]any{fixtureTaskKey: "bootstrap-delete"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/bootstrap-delete":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/maintenance-windows":
			response = map[string]any{"mode": "automatic", "windows": []any{}}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/pricing":
			response = map[string]any{"pricing": []any{}}
		case r.Method == http.MethodDelete && r.URL.Path == fixtureProParentPath && parent && !bootstrap:
			parent = false
			response = map[string]any{fixtureTaskKey: "parent-delete"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/parent-delete":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete}
		case r.Method == http.MethodGet && r.URL.Path == fixtureProParentPath && !parent:
			http.NotFound(w, r)
			return
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
	for _, k := range []string{fixtureTFCLIArgs, fixtureTFCLIArgsInit, fixtureTFCLIArgsApply, fixtureTFCLIArgsPlan, fixtureTFCLIArgsDestroy} {
		t.Setenv(k, "")
	}
	for _, k := range []string{fixtureHTTPProxy, fixtureHTTPSProxy, fixtureAllProxy, fixtureHTTPProxyLower, fixtureHTTPSProxyLower, fixtureAllProxyLower} {
		t.Setenv(k, "http://127.0.0.1:1")
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
	name, parentName, cidr, provider, payment, paymentID, storage, by := fixtureName, fixtureProParentName, "10.0.0.0/24", cloud, fixturePaymentType, "3", "ram", fixtureThroughputBy
	count, memory, throughput := float64(1), float64(1), float64(1000)
	replication := false
	item := &pro.Subscription{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("fixture-pro-parent-uid")}}
	item.Spec.ForProvider.Name = &parentName
	item.Spec.ForProvider.PaymentMethod = &payment
	item.Spec.ForProvider.PaymentMethodID = &paymentID
	item.Spec.ForProvider.MemoryStorage = &storage
	item.Spec.ForProvider.CloudProvider = []pro.CloudProviderParameters{{Provider: &provider, Region: []pro.RegionParameters{{Region: &region, NetworkingDeploymentCidr: &cidr}}}}
	item.Spec.ForProvider.CreationPlan = []pro.CreationPlanParameters{{Quantity: &count, MemoryLimitInGb: &memory, Replication: &replication, ThroughputMeasurementBy: &by, ThroughputMeasurementValue: &throughput}}
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	store := newTrackingNativeStore()
	spy := &spyExecutor{Interface: kexec.New(), tracked: store}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_subscription"], ujcontroller.WithCallbackProvider(callback))
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Subscription_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
	await := func(command string, expectFailure bool) {
		t.Helper()
		for i := 0; i < 8; i++ {
			if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-callback.done:
				if (err != nil) != expectFailure {
					mu.Lock()
					t.Logf("fake routes: %v", calls)
					mu.Unlock()
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
							mu.Lock()
							t.Logf("fake routes: %v", calls)
							mu.Unlock()
							t.Fatalf("unexpected operation result: %v", err)
						}
						return
					case <-time.After(180 * time.Second):
						t.Fatal("callback timeout")
					}
				}
			}
		}
		t.Fatal("callback missing")
	}
	await("terraform apply ", acceptedUnknown)
	if acceptedUnknown {
		verifyNoACLCreateReplay(t, rec, store, callback, name)
		mu.Lock()
		if !parent || !bootstrap || bootstrapRemoved || unexpected || createRequests != 1 || deleteRequests != 0 {
			mu.Unlock()
			t.Fatal("uncertain Pro subscription create was replayed, deleted or not retained")
		}
		mu.Unlock()
		for _, path := range []string{fixtureProParentPath, fixtureBootstrapPath} {
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
		current := &pro.Subscription{}
		if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, current); err != nil {
			t.Fatal(err)
		}
		if current.Status.AtProvider.ID != nil && *current.Status.AtProvider.ID == "1" {
			item = current
			break
		}
	}
	mu.Lock()
	if !parent || bootstrap || !bootstrapRemoved || unexpected {
		mu.Unlock()
		t.Fatal("bootstrap cleanup or parent identity not proved")
	}
	mu.Unlock()
	if item.Status.AtProvider.ID == nil || *item.Status.AtProvider.ID != "1" {
		t.Fatal("fake parent ID missing")
	}
	observed := item.Status.AtProvider.CloudProvider
	if len(observed) != 1 || observed[0].Provider == nil || *observed[0].Provider != cloud || len(observed[0].Region) != 1 || observed[0].Region[0].Region == nil || *observed[0].Region[0].Region != region {
		t.Fatal("managed subscription observation did not retain selected cloud and region")
	}
	for path, status := range map[string]int{fixtureBootstrapPath: http.StatusNotFound, fixtureProParentPath: http.StatusOK} {
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
			t.Fatalf("fake bootstrap read %s: %d", path, res.StatusCode)
		}
	}
	if err := kube.Delete(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	await("terraform destroy ", false)
	for i := 0; i < 30; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		if apierrors.IsNotFound(kube.Get(context.Background(), types.NamespacedName{Name: name}, &pro.Subscription{})) {
			break
		}
		if i == 29 {
			t.Fatal("fake parent CR remains")
		}
		time.Sleep(time.Second)
	}
	mu.Lock()
	if parent || bootstrap || unexpected {
		mu.Unlock()
		t.Fatal("fake residue")
	}
	mu.Unlock()
	for path, status := range map[string]int{fixtureProParentPath: http.StatusNotFound, fixtureSubscriptionsPath: http.StatusOK} {
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
			var inventory struct {
				Subscriptions []any `json:"subscriptions"`
			}
			err = json.NewDecoder(res.Body).Decode(&inventory)
			if err != nil || inventory.Subscriptions == nil || len(inventory.Subscriptions) != 0 {
				_ = res.Body.Close()
				t.Fatal("fake parent inventory incomplete or nonempty")
			}
		}
		_ = res.Body.Close()
	}
	if _, err := os.Stat(filepath.Join(dir, "fixture-pro-parent-uid")); !os.IsNotExist(err) {
		t.Fatal("fake workspace remains")
	}
}
