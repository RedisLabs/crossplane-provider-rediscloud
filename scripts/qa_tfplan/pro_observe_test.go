package qatfplan

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	xpmeta "github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	ujcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	ujresource "github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Fresh workspace, observe-only policy and an external ID select Upjet's
// Terraform import path; deleting the fake CR must retain the fake resource.
func verifyProObserveRetain(t *testing.T, item ujresource.Terraformed, scheme *runtime.Scheme, resourceType, id, url, exactPath, idKey string, numericID float64) { //nolint:gocyclo // Keep import, retain and identity assertions together.
	t.Helper()
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
	xpmeta.SetExternalName(item, id)
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	store := newTrackingNativeStore()
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: url + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources[resourceType], ujcontroller.WithCallbackProvider(callback))
	kinds, _, err := scheme.ObjectKinds(item)
	if err != nil || len(kinds) != 1 {
		t.Fatal("cannot resolve managed kind")
	}
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(kinds[0]), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithManagementPolicies())
	request := reconcile.Request{NamespacedName: types.NamespacedName{Name: item.GetName()}}
	for i := 0; i < 3; i++ {
		if _, err = rec.Reconcile(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	if err = kube.Get(context.Background(), request.NamespacedName, item); err != nil {
		t.Fatal(err)
	}
	observation, err := item.GetObservation()
	if err != nil || observation["id"] != id {
		t.Fatalf("fake import identity missing: %v", err)
	}
	if err = kube.Delete(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if _, err = rec.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err = kube.Get(context.Background(), request.NamespacedName, item); !apierrors.IsNotFound(err) {
		t.Fatal("observe-only fake CR was not removed")
	}
	req, err := http.NewRequest(http.MethodGet, url+exactPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", dummyCredential)
	req.Header.Set("X-Api-Secret-Key", dummyCredential)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var observed map[string]any
	err = json.NewDecoder(res.Body).Decode(&observed)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || observed[idKey] != numericID {
		t.Fatal("retained fake exact identity missing")
	}
}

func fakeCallCount(mu *sync.Mutex, calls *[]string) int {
	mu.Lock()
	defer mu.Unlock()
	return len(*calls)
}

func assertFakeGETOnly(t *testing.T, mu *sync.Mutex, calls *[]string, before int) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if len(*calls) <= before {
		t.Fatal("no authenticated fake GET observed")
	}
	for _, call := range (*calls)[before:] {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("unexpected fake write: %s", call)
		}
	}
}

func verifyProUnchanged(t *testing.T, rec *managed.Reconciler, store *trackingNativeStore, callback *fakeCallback, name string, mu *sync.Mutex, calls *[]string) {
	t.Helper()
	before := fakeCallCount(mu, calls)
	for i := 0; i < 3; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
			t.Fatal(err)
		}
		if store.current != nil && store.current.LastOperation.IsRunning() {
			select {
			case callbackErr := <-callback.done:
				_ = callbackErr
			case <-time.After(90 * time.Second):
				t.Fatal("unexpected async mutation did not finish")
			}
			t.Fatal("unchanged reconcile scheduled async mutation")
		}
		select {
		case callbackErr := <-callback.done:
			_ = callbackErr
			t.Fatal("unchanged reconcile produced async callback")
		default:
		}
		time.Sleep(time.Second)
	}
	assertFakeGETOnly(t, mu, calls, before)
}
