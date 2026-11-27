package qatfplan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	psc "github.com/RedisLabs/provider-rediscloud/apis/private/v1beta1"
	psccontroller "github.com/RedisLabs/provider-rediscloud/internal/controller/private/serviceconnectendpoint"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errPSCFixtureWrite = errors.New("synthetic API write outcome")
var errPSCFixtureDelete = errors.New("synthetic unknown delete outcome")

// This real API-server fixture checks reservation behavior with synthetic
// writer and inner client outcomes; it is not Cloud or multi-actor fencing.
func TestProductPSCHoldReservationAPI(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue {
		t.Skip("hosted Actions only")
	}
	for _, mode := range []string{"concurrent_success", "inner_error", "write_rejected", "write_committed_response_lost"} {
		t.Run(mode, func(t *testing.T) { runPSCHoldReservation(t, mode) })
	}
}

// This is an intentionally unsafe characterization, not a protection test:
// two separately persisted CRs targeting the same endpoint each reserve once.
func TestPSCHoldSameTargetTwoCRsCharacterization(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue {
		t.Skip("hosted Actions only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	scheme := runtime.NewScheme()
	if err := psc.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	first := &psc.ServiceConnectEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "psc-first", Finalizers: []string{managed.FinalizerName}}}
	sub, service := "1", float64(7)
	project, vpc, subnet, prefix := pscFixtureProject, pscFixtureVPC, pscFixtureSubnet, pscFixturePrefix
	first.Spec.ForProvider.SubscriptionID, first.Spec.ForProvider.PrivateServiceConnectServiceID = &sub, &service
	first.Spec.ForProvider.GCPProjectID, first.Spec.ForProvider.GCPVPCName = &project, &vpc
	first.Spec.ForProvider.GCPVPCSubnetName, first.Spec.ForProvider.EndpointConnectionName = &subnet, &prefix
	first.Spec.ProviderConfigReference = &xpv2.Reference{Name: pscTestProviderConfig}
	first.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	meta.SetExternalName(first, pscTestEndpointExternal)
	second := first.DeepCopy()
	second.Name = "psc-second"
	kube := newPSCEnvtestClient(t, scheme, first)
	if err := kube.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if first.UID == second.UID || first.UID == "" || second.UID == "" {
		t.Fatal("fixture requires two distinct API-server identities")
	}
	var calls atomic.Int32
	inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
		return managed.ExternalClientFns{DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
			calls.Add(1)
			return managed.ExternalDelete{}, errPSCFixtureDelete
		}}, nil
	})
	connector := psccontroller.NewDeleteHoldConnector(kube, kube, inner, true)
	for _, item := range []*psc.ServiceConnectEndpoint{first, second} {
		if err := kube.Delete(ctx, item); err != nil {
			t.Fatal(err)
		}
		current := &psc.ServiceConnectEndpoint{}
		if err := kube.Get(ctx, client.ObjectKeyFromObject(item), current); err != nil {
			t.Fatal(err)
		}
		external, err := connector.Connect(ctx, current)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = external.Delete(ctx, current.DeepCopy()); !errors.Is(err, errPSCFixtureDelete) {
			t.Fatalf("expected synthetic uncertain outcome, got %v", err)
		}
		if err := kube.Get(ctx, client.ObjectKeyFromObject(item), current); err != nil || current.Annotations[pscTestIntentAnnotation] == "" || !slices.Contains(current.Finalizers, managed.FinalizerName) {
			t.Fatal("expected each CR to retain an intent and finalizer")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("characterization changed: expected two same-target synthetic dispatches, got %d", calls.Load())
	}
	t.Log("UNSAFE CHARACTERIZATION: two CR-local reservations dispatched twice for the same PSC target; no Cloud call occurred")
}

func runPSCHoldReservation(t *testing.T, mode string) { //nolint:gocyclo // Every persisted outcome is checked independently.
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	scheme := runtime.NewScheme()
	if err := psc.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	item := &psc.ServiceConnectEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "psc-cas-fixture", Finalizers: []string{managed.FinalizerName}}}
	sub, service := "1", float64(7)
	project, vpc, subnet, prefix := pscFixtureProject, pscFixtureVPC, pscFixtureSubnet, pscFixturePrefix
	item.Spec.ForProvider.SubscriptionID, item.Spec.ForProvider.PrivateServiceConnectServiceID = &sub, &service
	item.Spec.ForProvider.GCPProjectID, item.Spec.ForProvider.GCPVPCName = &project, &vpc
	item.Spec.ForProvider.GCPVPCSubnetName, item.Spec.ForProvider.EndpointConnectionName = &subnet, &prefix
	item.Spec.ProviderConfigReference = &xpv2.Reference{Name: pscTestProviderConfig}
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	meta.SetExternalName(item, pscTestEndpointExternal)
	kube := newPSCEnvtestClient(t, scheme, item)
	if err := kube.Delete(ctx, item); err != nil {
		t.Fatal(err)
	}
	key := types.NamespacedName{Name: item.Name}
	current := &psc.ServiceConnectEndpoint{}
	if err := kube.Get(ctx, key, current); err != nil || current.UID != item.UID || current.GetDeletionTimestamp().IsZero() || !slices.Contains(current.Finalizers, managed.FinalizerName) || meta.GetExternalName(current) != pscTestEndpointExternal {
		t.Fatal("expected deleting PSC identity not persisted")
	}
	var calls atomic.Int32
	inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
		return managed.ExternalClientFns{DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
			calls.Add(1)
			if mode == "inner_error" {
				return managed.ExternalDelete{}, errPSCFixtureDelete
			}
			return managed.ExternalDelete{}, nil
		}}, nil
	})
	writer := &pscFaultWriter{Client: kube, mode: mode, ready: make(chan struct{})}
	connector := psccontroller.NewDeleteHoldConnector(kube, writer, inner, true)
	external, err := connector.Connect(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "concurrent_success" {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var successes atomic.Int32
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(snapshot *psc.ServiceConnectEndpoint) {
				defer wg.Done()
				<-start
				if _, deleteErr := external.Delete(ctx, snapshot); deleteErr == nil {
					successes.Add(1)
				}
			}(current.DeepCopy())
		}
		close(start)
		wg.Wait()
		if successes.Load() != 1 || calls.Load() != 1 || writer.attempts.Load() != 2 {
			t.Fatalf("reservation success=%d underlying deletes=%d write attempts=%d", successes.Load(), calls.Load(), writer.attempts.Load())
		}
	} else {
		if _, deleteErr := external.Delete(ctx, current.DeepCopy()); deleteErr == nil {
			t.Fatal("expected modeled error")
		}
		wantCalls := int32(0)
		if mode == "inner_error" {
			wantCalls = 1
		}
		if calls.Load() != wantCalls {
			t.Fatalf("unexpected underlying deletes=%d", calls.Load())
		}
	}
	retained := &psc.ServiceConnectEndpoint{}
	if err := kube.Get(ctx, key, retained); err != nil || retained.UID != current.UID || !slices.Contains(retained.Finalizers, managed.FinalizerName) {
		t.Fatal("deleting PSC UID or finalizer lost")
	}
	intent, held := retained.Annotations[pscTestIntentAnnotation]
	if mode == "write_rejected" {
		if held || writer.attempts.Load() != 1 {
			t.Fatal("rejected pre-write unexpectedly persisted a marker")
		}
		// Only this injected failure is known to be before the API call. A real
		// error alone cannot establish that a reservation was not committed.
		retryConnector := psccontroller.NewDeleteHoldConnector(kube, kube, inner, true)
		retry, retryErr := retryConnector.Connect(ctx, retained)
		if retryErr != nil {
			t.Fatal(retryErr)
		}
		if _, retryErr = retry.Delete(ctx, retained.DeepCopy()); retryErr != nil || calls.Load() != 1 {
			t.Fatal("known pre-write rejection could not retry once")
		}
		if err := kube.Get(ctx, key, retained); err != nil || retained.Annotations[pscTestIntentAnnotation] == "" {
			t.Fatal("retry reservation missing")
		}
		if _, retryErr = retry.Delete(ctx, retained.DeepCopy()); retryErr == nil || calls.Load() != 1 {
			t.Fatal("retry reservation allowed replay")
		}
		return
	}
	if !held {
		t.Fatal("expected persisted reservation missing")
	}
	var marker struct {
		UID, External, Provider, Token string
		Version                        int
	}
	if json.Unmarshal([]byte(intent), &marker) != nil || marker.Version != 1 || marker.UID != string(current.UID) || marker.External != pscTestEndpointExternal || marker.Provider != pscTestProviderConfig || marker.Token == "" {
		t.Fatal("persisted reservation identity mismatch")
	}
	before := calls.Load()
	if _, err := external.Delete(ctx, current.DeepCopy()); err == nil || calls.Load() != before {
		t.Fatal("held reservation allowed replay")
	}
}

// Rejection happens before the API call; lost response happens after a real
// committed API update. Neither models a production Kubernetes network fault.
type pscFaultWriter struct {
	client.Client
	mode     string
	attempts atomic.Int32
	ready    chan struct{}
}

func (w *pscFaultWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	attempt := w.attempts.Add(1)
	if w.mode == "concurrent_success" {
		if attempt == 2 {
			close(w.ready)
		}
		select {
		case <-w.ready:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if w.mode == "write_rejected" {
		return errPSCFixtureWrite
	}
	if err := w.Client.Update(ctx, obj, opts...); err != nil {
		return err
	}
	if w.mode == "write_committed_response_lost" {
		return errPSCFixtureWrite
	}
	return nil
}
