package qatfplan

import (
	"context"
	"os"
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

// This uses a real API server and managed reconciler but a synthetic external
// connector. In particular, nil from Delete models scheduling before dispatch;
// it is not a claim about an actual Terraform command or Cloud task.
func TestProductProHoldPolicyBoundaries(t *testing.T) { //nolint:gocyclo // Each policy boundary is checked independently.
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("hosted Actions only")
	}
	for _, mode := range []string{"orphan", "no_delete", "nil_before_dispatch"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := pro.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			item := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: "policy-fixture", Finalizers: []string{managed.FinalizerName}}}
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
			if mode == "orphan" {
				item.Spec.DeletionPolicy = xpv2.DeletionOrphan
			}
			if mode == "no_delete" {
				item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
			}
			if mode != "nil_before_dispatch" {
				item.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"] = "held"
			}
			kube := newProEnvtestClient(t, scheme, item)
			if err := kube.Delete(ctx, item); err != nil {
				t.Fatal(err)
			}
			connects, deletes := 0, 0
			inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
				connects++
				return managed.ExternalClientFns{
					DisconnectFn: func(context.Context) error { return nil },
					ObserveFn: func(context.Context, xpresource.Managed) (managed.ExternalObservation, error) {
						return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: true}, nil
					},
					DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
						deletes++
						return managed.ExternalDelete{}, nil
					},
				}, nil
			})
			mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
			connector := procontroller.NewDeleteHoldConnector(kube, kube, inner, true)
			rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithManagementPolicies())
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: item.Name}}
			for i := 0; i < 4; i++ {
				if _, err := rec.Reconcile(ctx, req); err != nil {
					t.Fatal(err)
				}
			}
			got := &pro.Database{}
			err := kube.Get(ctx, req.NamespacedName, got)
			if mode != "nil_before_dispatch" {
				if !apierrors.IsNotFound(err) || connects != 0 || deletes != 0 {
					t.Fatalf("orphan bypass: get=%v connects=%d deletes=%d", err, connects, deletes)
				}
				return
			}
			if err != nil || len(got.Finalizers) == 0 {
				t.Fatalf("held object or finalizer lost: %v", err)
			}
			if _, ok := got.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"]; !ok || deletes != 1 {
				t.Fatalf("nil-before-dispatch hold missing or replayed: deletes=%d", deletes)
			}
		})
	}
}
