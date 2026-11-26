package qatfplan

import (
	"context"
	"os"
	"reflect"
	"testing"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	procontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/subscription/database"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// This models only an administrator's Kubernetes policy transition. The fake
// connector is never called; it does not establish Cloud deletion or authority.
func TestProductProHeldPolicyTransition(t *testing.T) { //nolint:gocyclo // Each failure and transition boundary is asserted.
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue {
		t.Skip("hosted Actions only")
	}
	for _, mode := range []string{"orphan", "no_delete"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := pro.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			item := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: "transition-fixture", Finalizers: []string{managed.FinalizerName}}}
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
			item.Spec.ProviderConfigReference = &xpv2.Reference{Name: fixtureName + "-config"}
			item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
			meta.SetExternalName(item, fixtureProDBID)
			item.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"] = "held"
			kube := newProEnvtestClient(t, scheme, item)
			if err := kube.Delete(ctx, item); err != nil {
				t.Fatal(err)
			}
			key := types.NamespacedName{Name: item.Name}
			held := &pro.Database{}
			if err := kube.Get(ctx, key, held); err != nil {
				t.Fatal(err)
			}
			originalUID := held.UID
			assertHeld := func() *pro.Database {
				t.Helper()
				got := &pro.Database{}
				if err := kube.Get(ctx, key, got); err != nil {
					t.Fatal(err)
				}
				if got.UID != originalUID || got.GetDeletionTimestamp().IsZero() || len(got.Finalizers) == 0 || got.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"] != "held" {
					t.Fatal("held identity, marker, or finalizer lost")
				}
				return got
			}
			invalid := held.DeepCopy()
			invalid.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementAction("invalid")})
			if err := kube.Update(ctx, invalid); err == nil || !apierrors.IsInvalid(err) {
				t.Fatalf("expected admission invalid: %v", err)
			}
			held = assertHeld()
			wrongUID := held.DeepCopy()
			wrongUID.UID = types.UID("different-uid")
			if err := kube.Update(ctx, wrongUID); err == nil {
				t.Fatal("changed UID update accepted")
			}
			held = assertHeld()
			stale := held.DeepCopy()
			fresh := held.DeepCopy()
			fresh.Annotations["fixture.test/version"] = "advanced"
			if err := kube.Update(ctx, fresh); err != nil {
				t.Fatal(err)
			}
			stale.Spec.DeletionPolicy = xpv2.DeletionOrphan
			if err := kube.Update(ctx, stale); !apierrors.IsConflict(err) {
				t.Fatalf("expected stale-version conflict: %v", err)
			}
			held = assertHeld()
			if mode == "orphan" {
				held.Spec.DeletionPolicy = xpv2.DeletionOrphan
			} else {
				held.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
			}
			if err := kube.Update(ctx, held); err != nil {
				t.Fatalf("terminating policy update failed: %v", err)
			}
			connects := 0
			inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
				connects++
				return managed.ExternalClientFns{}, nil
			})
			mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
			connector := procontroller.NewDeleteHoldConnector(kube, kube, inner, true)
			rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithManagementPolicies())
			req := reconcile.Request{NamespacedName: key}
			if _, err := rec.Reconcile(ctx, req); err != nil {
				t.Fatal(err)
			}
			if err := kube.Get(ctx, key, &pro.Database{}); !apierrors.IsNotFound(err) || connects != 0 {
				t.Fatalf("expected same held CR finalized without connector: get=%v connects=%d", err, connects)
			}
			// Recreate the same name with a new server UID; an old snapshot must
			// not mutate the replacement, even if the name is reused.
			replacement := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: item.Name, Finalizers: []string{managed.FinalizerName}, Annotations: map[string]string{"rediscloud.crossplane.io/pro-database-delete-intent": "replacement"}}, Spec: item.Spec}
			meta.SetExternalName(replacement, fixtureProDBID)
			if err := kube.Create(ctx, replacement); err != nil {
				t.Fatal(err)
			}
			if replacement.UID == "" || replacement.UID == originalUID {
				t.Fatal("replacement UID not assigned")
			}
			if err := kube.Update(ctx, stale); err == nil {
				t.Fatal("old snapshot updated replacement")
			}
			oldUIDCurrentVersion := replacement.DeepCopy()
			oldUIDCurrentVersion.UID = originalUID
			if err := kube.Update(ctx, oldUIDCurrentVersion); err == nil {
				t.Fatal("old UID updated replacement with current resourceVersion")
			}
			current := &pro.Database{}
			if err := kube.Get(ctx, key, current); err != nil {
				t.Fatal(err)
			}
			if current.UID != replacement.UID || current.ResourceVersion != replacement.ResourceVersion || !reflect.DeepEqual(current.Spec, replacement.Spec) || len(current.Finalizers) == 0 || current.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"] != "replacement" {
				t.Fatal("replacement changed")
			}
		})
	}
}
