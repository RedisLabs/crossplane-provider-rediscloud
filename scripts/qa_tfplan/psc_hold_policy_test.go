package qatfplan

import (
	"context"
	"os"
	"slices"
	"testing"

	psc "github.com/RedisLabs/provider-rediscloud/apis/private/v1beta1"
	psccontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/private/serviceconnectendpoint"
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

// Orphan and management policies can bypass the connector entirely. This uses
// a real API server and managed reconciler, but the inner connector is only a
// call counter: disappearance of the CR says nothing about Cloud or GCP.
func TestProductPSCHoldPolicyBoundaries(t *testing.T) { //nolint:gocyclo // Each persisted-state and bypass assertion is intentional.
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue {
		t.Skip("hosted Actions only")
	}
	for _, mode := range []string{"orphan", "no_delete"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := psc.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			item := &psc.ServiceConnectEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "psc-policy-fixture", Finalizers: []string{managed.FinalizerName}}}
			sub, service := "1", float64(7)
			project, vpc, subnet, prefix := pscFixtureProject, pscFixtureVPC, pscFixtureSubnet, pscFixturePrefix
			item.Spec.ForProvider.SubscriptionID, item.Spec.ForProvider.PrivateServiceConnectServiceID = &sub, &service
			item.Spec.ForProvider.GCPProjectID, item.Spec.ForProvider.GCPVPCName = &project, &vpc
			item.Spec.ForProvider.GCPVPCSubnetName, item.Spec.ForProvider.EndpointConnectionName = &subnet, &prefix
			item.Spec.ProviderConfigReference = &xpv2.Reference{Name: pscTestProviderConfig}
			item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
			meta.SetExternalName(item, pscTestEndpointExternal)
			item.Annotations[pscTestIntentAnnotation] = pscTestHeldMarker
			if mode == "orphan" {
				item.Spec.DeletionPolicy = xpv2.DeletionOrphan
			}
			if mode == "no_delete" {
				item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
			}
			kube := newPSCEnvtestClient(t, scheme, item)
			key := types.NamespacedName{Name: item.Name}
			persisted := &psc.ServiceConnectEndpoint{}
			if err := kube.Get(ctx, key, persisted); err != nil || persisted.UID == "" || !slices.Contains(persisted.Finalizers, managed.FinalizerName) || persisted.Annotations[pscTestIntentAnnotation] != pscTestHeldMarker || meta.GetExternalName(persisted) != pscTestEndpointExternal {
				t.Fatal("expected held PSC fixture was not persisted")
			}
			if mode == "orphan" && persisted.Spec.DeletionPolicy != xpv2.DeletionOrphan {
				t.Fatal("orphan policy was not persisted")
			}
			if mode == "no_delete" && !slices.Equal(persisted.GetManagementPolicies(), xpv2.ManagementPolicies{xpv2.ManagementActionObserve}) {
				t.Fatal("observe-only policy was not persisted")
			}
			if err := kube.Delete(ctx, persisted); err != nil {
				t.Fatal(err)
			}
			deleting := &psc.ServiceConnectEndpoint{}
			if err := kube.Get(ctx, key, deleting); err != nil || deleting.UID != persisted.UID || deleting.GetDeletionTimestamp().IsZero() || !slices.Contains(deleting.Finalizers, managed.FinalizerName) || deleting.Annotations[pscTestIntentAnnotation] != pscTestHeldMarker {
				t.Fatal("held PSC did not enter deletion before reconciliation")
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
			connector := psccontroller.NewDeleteHoldConnector(kube, kube, inner, true)
			rec := managed.NewReconciler(mgr, xpresource.ManagedKind(psc.ServiceConnectEndpoint_GroupVersionKind), managed.WithExternalConnector(connector), managed.WithInitializers(), managed.WithManagementPolicies())
			req := reconcile.Request{NamespacedName: key}
			for i := 0; i < 4; i++ {
				if _, err := rec.Reconcile(ctx, req); err != nil {
					t.Fatal(err)
				}
			}
			err := kube.Get(ctx, key, &psc.ServiceConnectEndpoint{})
			if !apierrors.IsNotFound(err) || connects != 0 || deletes != 0 {
				t.Fatalf("policy bypass: get=%v connects=%d deletes=%d", err, connects, deletes)
			}
		})
	}
}
