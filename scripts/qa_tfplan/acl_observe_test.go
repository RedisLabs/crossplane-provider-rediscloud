package qatfplan

import (
	"context"
	"testing"

	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	xpmeta "github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	ujcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	ujresource "github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// A new observe-only managed object with an external ID forces Upjet's real
// Terraform import path into a fresh native workspace. The fixture server is
// responsible for counting all HTTP methods, including unexpected writes.
func verifyACLObserveOnly(t *testing.T, item ujresource.Terraformed, scheme *runtime.Scheme, resourceType string, url string, extras ...client.Object) {
	t.Helper()
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
	xpmeta.SetExternalName(item, "42")
	objects := append([]client.Object{item}, extras...)
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(objects...).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	store := newTrackingNativeStore()
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: fixtureTerraformVersion, Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: url + "/v1"}}, nil
	}
	callback := &fakeCallback{done: make(chan error, 1)}
	connector := ujcontroller.NewConnector(kube, store, setup, providerconfig.GetProvider().Resources[resourceType], ujcontroller.WithCallbackProvider(callback))
	gvk := item.GetObjectKind().GroupVersionKind()
	// Fake objects may not carry TypeMeta; the registered scheme determines it.
	if gvk.Empty() {
		kinds, _, err := scheme.ObjectKinds(item)
		if err != nil || len(kinds) != 1 {
			t.Fatal("cannot resolve managed kind")
		}
		gvk = kinds[0]
	}
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(gvk), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithManagementPolicies())
	for i := 0; i < 3; i++ {
		if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: item.GetName()}}); err != nil {
			t.Fatalf("observe-only reconcile: %v", err)
		}
	}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: item.GetName()}, item); err != nil {
		t.Fatal(err)
	}
	observation, err := item.GetObservation()
	if err != nil || observation["id"] != "42" {
		t.Fatalf("import observation missing exact ID: %v", err)
	}
}
