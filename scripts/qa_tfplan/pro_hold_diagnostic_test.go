package qatfplan

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	procontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/subscription/database"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const diagnosticSecretSentinel = "private-fixture-token-should-not-appear"
const diagnosticAdvice = "preserve the resource and any delete hold, and contact an administrator"

type failedDiagnosticReader struct{ client.Reader }

func (r failedDiagnosticReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New(diagnosticSecretSentinel)
}

// The API-server status is real; fake recorder captures the event that the
// managed reconciler emits. There are no Cloud credentials or requests.
func TestProductProHoldDiagnostic(t *testing.T) { //nolint:gocyclo // Both distinct safety failures need all output assertions.
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue {
		t.Skip("hosted Actions only")
	}
	for _, mode := range []string{"marker", "read_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := pro.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			item := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: "diagnostic-fixture", Finalizers: []string{managed.FinalizerName}}}
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
			if mode == "marker" {
				item.Annotations = map[string]string{"rediscloud.crossplane.io/pro-database-delete-intent": diagnosticSecretSentinel}
			}
			kube := newProEnvtestClient(t, scheme, item)
			if err := kube.Delete(ctx, item); err != nil {
				t.Fatal(err)
			}
			var reader client.Reader = kube
			if mode == "read_failure" {
				reader = failedDiagnosticReader{Reader: kube}
			}
			connects := 0
			inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
				connects++
				return managed.ExternalClientFns{}, nil
			})
			recorder := record.NewFakeRecorder(8)
			mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
			connector := procontroller.NewDeleteHoldConnector(reader, kube, inner, true)
			rec := managed.NewReconciler(mgr, xpresource.ManagedKind(pro.Database_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithRecorder(event.NewAPIRecorder(recorder)))
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: item.Name}}
			if _, err := rec.Reconcile(ctx, req); err != nil {
				t.Fatal(err)
			}
			current := &pro.Database{}
			if err := kube.Get(ctx, req.NamespacedName, current); err != nil {
				t.Fatal(err)
			}
			condition := current.GetCondition(xpv2.TypeSynced)
			if condition.Status != corev1.ConditionFalse || condition.Reason != xpv2.ReasonReconcileError || !strings.Contains(condition.Message, diagnosticAdvice) || strings.Contains(condition.Message, diagnosticSecretSentinel) || connects != 0 || len(current.Finalizers) == 0 {
				t.Fatalf("unexpected status or safety behavior: status=%s reason=%s connects=%d", condition.Status, condition.Reason, connects)
			}
			select {
			case recorded := <-recorder.Events:
				if !strings.Contains(recorded, "CannotConnectToProvider") || !strings.Contains(recorded, diagnosticAdvice) || strings.Contains(recorded, diagnosticSecretSentinel) {
					t.Fatal("event missing safe guidance or contained sentinel")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("expected warning event missing")
			}
		})
	}
}
