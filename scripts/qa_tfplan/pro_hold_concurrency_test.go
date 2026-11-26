package qatfplan

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	procontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/subscription/database"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestProductProHoldConcurrentReservation(t *testing.T) { //nolint:gocyclo // All API-server race preconditions are checked.
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue {
		t.Skip("hosted Actions only")
	}
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := pro.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	item := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: "cas-fixture", Finalizers: []string{managed.FinalizerName}}}
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
	meta.SetExternalName(item, fixtureProDBID)
	kube := newProEnvtestClient(t, scheme, item)
	if err := kube.Delete(ctx, item); err != nil {
		t.Fatal(err)
	}
	current := &pro.Database{}
	if err := kube.Get(ctx, types.NamespacedName{Name: item.Name}, current); err != nil {
		t.Fatal(err)
	}
	var dispatched atomic.Int32
	inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
		return managed.ExternalClientFns{DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
			dispatched.Add(1)
			return managed.ExternalDelete{}, nil
		}}, nil
	})
	connector := procontroller.NewDeleteHoldConnector(kube, kube, inner, true)
	external, err := connector.Connect(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(snapshot *pro.Database) {
			defer wg.Done()
			<-start
			if _, err := external.Delete(ctx, snapshot); err == nil {
				successes.Add(1)
			}
		}(current.DeepCopy())
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 || dispatched.Load() != 1 {
		t.Fatalf("successful reservations=%d underlying calls=%d", successes.Load(), dispatched.Load())
	}
	retained := &pro.Database{}
	if err := kube.Get(ctx, types.NamespacedName{Name: item.Name}, retained); err != nil {
		t.Fatal(err)
	}
	if _, ok := retained.Annotations["rediscloud.crossplane.io/pro-database-delete-intent"]; !ok || len(retained.Finalizers) == 0 {
		t.Fatal("reservation or finalizer missing")
	}
}
