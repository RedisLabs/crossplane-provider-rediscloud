package qatfplan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	api "github.com/RedisLabs/provider-rediscloud/apis/private/v1beta1"
	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	psccontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/private/serviceconnectendpoint"
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

// This fixture starts with an existing fake Pro parent. All writes target the
// local fake PSC endpoint; no AWS, Cloud account or live lock is exercised.
func TestPSCManagedEndpointLifecycleLoopback(t *testing.T) {
	runPSCManagedEndpoint(t, false, false)
}

func TestPSCManagedEndpointFailureLoopback(t *testing.T) {
	runPSCManagedEndpoint(t, true, false)
}

func TestPSCManagedEndpointAcceptedThen503Loopback(t *testing.T) {
	runPSCManagedEndpoint(t, false, true)
}

func TestProductPSCEndpointDeleteHold(t *testing.T) {
	for _, tc := range []struct {
		name             string
		enabled, unknown bool
	}{
		{"default_off", false, false}, {"success_hold", true, false}, {"accepted_503_hold", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("QA_PSC_HOLD_ENVTEST", fixtureStringTrue)
			if tc.enabled {
				t.Setenv("QA_PSC_HOLD_ENABLED", fixtureStringTrue)
			}
			if tc.unknown {
				t.Setenv("QA_PSC_HOLD_UNKNOWN", fixtureStringTrue)
			}
			runPSCManagedEndpoint(t, false, false)
		})
	}
}

func runPSCManagedEndpoint(t *testing.T, injectFailure, acceptedUnknown bool) { //nolint:gocyclo // Keep the isolated fake API and managed lifecycle together.
	requireHostedFixture(t)
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" && plugins == "" {
		t.Skip("pinned Terraform fixture not configured")
	}
	if tf == "" || plugins == "" {
		t.Fatal("pinned Terraform and provider are required")
	}
	const parentPath = "/v1/subscriptions/1/private-service-connect"
	const path = parentPath + "/7"
	var mu sync.Mutex
	exists, created, deleted, unexpected, failCreate := false, false, false, false, injectFailure
	createRequests, deleteRequests := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == path {
			createRequests++
		}
		if r.Method == http.MethodDelete {
			deleteRequests++
		}
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
		case r.Method == http.MethodGet && r.URL.Path == parentPath:
			response = map[string]any{fixtureTaskKey: "get-service"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-service":
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureResourceKey: map[string]any{"id": 7, fixtureStatusKey: fixtureActive}}}
		case r.Method == http.MethodGet && r.URL.Path == path:
			response = map[string]any{fixtureTaskKey: "get-endpoints"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-endpoints":
			endpoints := []any{}
			if exists {
				endpoints = append(endpoints, map[string]any{"id": 8, "gcpProjectId": "fixture-project", "gcpVpcName": "fixture-vpc", "gcpVpcSubnetName": "fixture-subnet", "endpointConnectionName": "fixture-prefix", fixtureStatusKey: "initialized"})
			}
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureResourceKey: map[string]any{"pscServiceId": 7, "endpoints": endpoints}}}
		case r.Method == http.MethodGet && r.URL.Path == path+"/endpoints/8/creationScripts" && exists:
			response = map[string]any{fixtureTaskKey: "get-script"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-script" && exists:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{fixtureResourceKey: map[string]any{"script": map[string]any{"terraformGcp": map[string]any{"serviceAttachments": []any{}}}}}}
		case r.Method == http.MethodPost && r.URL.Path == path && !exists && !created:
			if failCreate {
				failCreate = false
				http.Error(w, "injected fixture failure", http.StatusBadRequest)
				return
			}
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["gcpProjectId"] != "fixture-project" || body["gcpVpcName"] != "fixture-vpc" || body["gcpVpcSubnetName"] != "fixture-subnet" || body["endpointConnectionName"] != "fixture-prefix" {
				unexpected = true
				http.Error(w, "invalid endpoint fixture create", http.StatusBadRequest)
				return
			}
			exists, created = true, true
			if acceptedUnknown {
				http.Error(w, "accepted fixture response lost", http.StatusServiceUnavailable)
				return
			}
			response = map[string]any{fixtureTaskKey: "create-endpoint"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/create-endpoint" && created:
			response = map[string]any{fixtureStatusKey: fixtureTaskComplete, fixtureResponseKey: map[string]any{"resourceId": 8}}
		case r.Method == http.MethodDelete && r.URL.Path == path+"/endpoints/8" && exists && !deleted:
			if os.Getenv("QA_PSC_HOLD_UNKNOWN") == fixtureStringTrue {
				http.Error(w, "accepted pending deletion with unavailable response", http.StatusServiceUnavailable)
				return
			}
			exists, deleted = false, true
			response = map[string]any{fixtureTaskKey: "delete-endpoint"}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/delete-endpoint" && deleted:
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
	item := &api.ServiceConnectEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "fixture-endpoint", UID: types.UID("fixture-endpoint-uid")}}
	sub := "1"
	item.Spec.ForProvider.SubscriptionID = &sub
	serviceID := float64(7)
	project, vpc, subnet, prefix := "fixture-project", "fixture-vpc", "fixture-subnet", "fixture-prefix"
	item.Spec.ForProvider.PrivateServiceConnectServiceID = &serviceID
	item.Spec.ForProvider.GCPProjectID, item.Spec.ForProvider.GCPVPCName = &project, &vpc
	item.Spec.ForProvider.GCPVPCSubnetName, item.Spec.ForProvider.EndpointConnectionName = &subnet, &prefix
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	item.Spec.ProviderConfigReference = &xpv2.Reference{Name: "fixture-config"}
	var kube client.Client
	if os.Getenv("QA_PSC_HOLD_ENVTEST") == fixtureStringTrue {
		kube = newPSCEnvtestClient(t, scheme, item)
	} else {
		kube = cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	}
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	tagged := &pscHoldCallbacks{base: callback, kinds: make(chan string, 8)}
	buildConnector := func(store *trackingNativeStore, callback *fakeCallback, enabled bool) managed.ExternalConnector {
		tagged = &pscHoldCallbacks{base: callback, kinds: make(chan string, 8)}
		inner := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources["rediscloud_private_service_connect_endpoint"], ujcontroller.WithCallbackProvider(tagged))
		return psccontroller.NewDeleteHoldConnector(kube, kube, inner, enabled)
	}
	connector := buildConnector(store, callback, os.Getenv("QA_PSC_HOLD_ENABLED") == fixtureStringTrue)
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(api.ServiceConnectEndpoint_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: item.Name}}
	await := func(label string, wantError bool) {
		t.Helper()
		for i := 0; i < 8; i++ {
			if _, err := rec.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-callback.done:
				kind := <-tagged.kinds
				if (label == "create" && kind != "create") || (label == "delete" && kind != "destroy") {
					t.Fatalf("%s callback kind=%s", label, kind)
				}
				if (err != nil) != wantError {
					t.Fatalf("%s failed: %v", label, err)
				}
				return
			case <-time.After(2 * time.Second):
			}
			if store.current != nil && store.current.LastOperation != nil && store.current.LastOperation.IsRunning() {
				select {
				case err := <-callback.done:
					kind := <-tagged.kinds
					if (label == "create" && kind != "create") || (label == "delete" && kind != "destroy") {
						t.Fatalf("%s callback kind=%s", label, kind)
					}
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
	if acceptedUnknown {
		await("accepted create with unknown response", true)
		// Reuse the managed-operation observer to drain any scheduled replay.
		verifyNoACLCreateReplay(t, rec, store, callback, item.Name)
		mu.Lock()
		defer mu.Unlock()
		if !exists || !created || deleted || unexpected || createRequests != 1 || deleteRequests != 0 {
			t.Fatalf("accepted unknown create: exists=%v created=%v deleted=%v unexpected=%v creates=%d deletes=%d", exists, created, deleted, unexpected, createRequests, deleteRequests)
		}
		return
	}
	if injectFailure {
		await("injected endpoint failure", true)
		for i := 0; i < 3; i++ {
			if _, err := rec.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if exists || created || unexpected {
			t.Fatal("uncertain endpoint create was automatically replayed")
		}
		t.Log("SIMULATED: endpoint create failure remained fenced; recovery requires an independent absence check")
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
		if item.Status.AtProvider.ID != nil && *item.Status.AtProvider.ID == "1/7/8" && len(item.Finalizers) != 0 {
			break
		}
	}
	if item.Status.AtProvider.ID == nil || *item.Status.AtProvider.ID != "1/7/8" || len(item.Finalizers) == 0 {
		t.Fatal("fake PSC identity/finalizer missing")
	}
	mu.Lock()
	if !exists || !created || unexpected {
		mu.Unlock()
		t.Fatal("unexpected fake create")
	}
	mu.Unlock()
	t.Log("SIMULATED: managed fake PSC endpoint created and read; fake parent present")
	originalUID := item.UID
	if err := kube.Delete(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	await("delete", os.Getenv("QA_PSC_HOLD_UNKNOWN") == fixtureStringTrue)
	if os.Getenv("QA_PSC_HOLD_ENABLED") == fixtureStringTrue {
		store = newTrackingNativeStore()
		callback = &fakeCallback{done: make(chan error, 1)}
		connector = buildConnector(store, callback, false)
		rec = managed.NewReconciler(mgr, xpresource.ManagedKind(api.ServiceConnectEndpoint_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithFinalizer(terraform.NewWorkspaceFinalizer(store.inner, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))))
		for i := 0; i < 3; i++ {
			if _, err := rec.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			current := &api.ServiceConnectEndpoint{}
			if err := kube.Get(context.Background(), req.NamespacedName, current); err != nil || current.UID != originalUID || !slices.Contains(current.Finalizers, managed.FinalizerName) || current.GetDeletionTimestamp().IsZero() {
				t.Fatal("held CR or finalizer lost")
			}
			if _, ok := current.Annotations["rediscloud.crossplane.io/psc-endpoint-delete-intent"]; !ok {
				t.Fatal("PSC delete intent lost")
			}
			if store.current != nil {
				t.Fatal("held reconcile opened upstream workspace")
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if unexpected || deleteRequests != 1 || !created || (os.Getenv("QA_PSC_HOLD_UNKNOWN") == fixtureStringTrue && !exists) || (os.Getenv("QA_PSC_HOLD_UNKNOWN") != fixtureStringTrue && exists) {
			t.Fatalf("unexpected held PSC state: deletes=%d exists=%v", deleteRequests, exists)
		}
		return
	}
	mu.Lock()
	if exists || !deleted || unexpected {
		mu.Unlock()
		t.Fatal("unexpected fake delete")
	}
	mu.Unlock()
	// Endpoint reads are list-based: assert the modeled list is empty while
	// the service and subscription remain readable; do not claim child 404.
	for _, target := range []string{path, parentPath, fixtureProParentPath} {
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
		if response.StatusCode != http.StatusOK {
			t.Fatalf("fake parent/list read %s status=%d", target, response.StatusCode)
		}
	}
	// The asynchronous workspace/finalizer can need more than 15 reconciles on
	// a busy hosted runner. Keep a bounded wait and require the same cleanup proof.
	for i := 0; i < 60; i++ {
		if _, err := rec.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		err := kube.Get(context.Background(), req.NamespacedName, &api.ServiceConnectEndpoint{})
		if apierrors.IsNotFound(err) {
			if _, statErr := os.Stat(filepath.Join(dir, string(item.UID))); !os.IsNotExist(statErr) {
				t.Fatal("fake workspace remains or cannot be checked")
			}
			t.Log("SIMULATED: fake PSC endpoint removed from modeled list with fake parent still present")
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	current := &api.ServiceConnectEndpoint{}
	if err := kube.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("managed PSC object still exists after delete; finalizers=%v conditions=%v", current.Finalizers, current.Status.Conditions)
}

type pscHoldCallbacks struct {
	base  *fakeCallback
	kinds chan string
}

func (c *pscHoldCallbacks) result(kind string) terraform.CallbackFn {
	return func(err error, ctx context.Context) error { c.kinds <- kind; return c.base.result()(err, ctx) }
}
func (c *pscHoldCallbacks) Create(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("create")
}
func (c *pscHoldCallbacks) Update(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("update")
}
func (c *pscHoldCallbacks) Destroy(types.NamespacedName, bool) terraform.CallbackFn {
	return c.result("destroy")
}
