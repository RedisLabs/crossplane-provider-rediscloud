// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package serviceconnectendpoint

import (
	"context"
	"math"
	"testing"

	pscv1 "github.com/RedisLabs/provider-rediscloud/apis/private/v1beta1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeleteHoldConnector(t *testing.T) { //nolint:gocyclo // Each safe failure boundary is asserted independently.
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := pscv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	makeObject := func() *pscv1.ServiceConnectEndpoint {
		now := metav1.Now()
		parent := "1"
		service := float64(7)
		obj := &pscv1.ServiceConnectEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "fixture", UID: types.UID("fixture-uid"), DeletionTimestamp: &now, Finalizers: []string{managed.FinalizerName}}}
		obj.Spec.ProviderConfigReference = &xpv2.Reference{Name: "fixture-config"}
		obj.Spec.ForProvider.SubscriptionID = &parent
		obj.Spec.ForProvider.PrivateServiceConnectServiceID = &service
		meta.SetExternalName(obj, "1/7/42")
		return obj
	}
	for _, tc := range []struct {
		name              string
		enabled, existing bool
		wantDelete        bool
	}{
		{"normal", false, false, true}, {"opt_in", true, false, true}, {"held_after_flag_off", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := makeObject()
			if tc.existing {
				item.Annotations[deleteIntentAnnotation] = ""
			}
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(item).Build()
			connects, deletes := 0, 0
			underlying := managed.ExternalClientFns{DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
				deletes++
				return managed.ExternalDelete{}, nil
			}}
			inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
				connects++
				return underlying, nil
			})
			connector := &deleteHoldConnector{inner: inner, reader: kube, writer: kube, enabled: tc.enabled}
			external, err := connector.Connect(ctx, item)
			if tc.existing {
				if err == nil || connects != 0 {
					t.Fatal("held resource reached upstream connect")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := external.Delete(ctx, item); err != nil {
				t.Fatal(err)
			}
			if (deletes == 1) != tc.wantDelete {
				t.Fatalf("unexpected delete count %d", deletes)
			}
			stored := &pscv1.ServiceConnectEndpoint{}
			if err := kube.Get(ctx, types.NamespacedName{Name: item.Name}, stored); err != nil {
				t.Fatal(err)
			}
			_, held := stored.Annotations[deleteIntentAnnotation]
			if held != tc.enabled {
				t.Fatalf("intent present=%v", held)
			}
			if tc.enabled {
				// Even if the inner client returned nil before dispatch, the intent cannot be reacquired.
				connector.enabled = false
				if _, err := connector.Connect(ctx, stored); err == nil || connects != 1 {
					t.Fatal("existing intent was bypassed after flag off")
				}
				if _, err := external.Delete(ctx, stored); err == nil || deletes != 1 {
					t.Fatal("existing intent was dispatched twice")
				}
			}
		})
	}
	t.Run("stale_nondeleting_snapshot", func(t *testing.T) {
		current := makeObject()
		current.Annotations[deleteIntentAnnotation] = "held"
		stale := current.DeepCopy()
		stale.DeletionTimestamp = nil
		delete(stale.Annotations, deleteIntentAnnotation)
		kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
		connects := 0
		inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) {
			connects++
			return managed.ExternalClientFns{}, nil
		})
		connector := &deleteHoldConnector{inner: inner, reader: kube, writer: kube, enabled: true}
		if _, err := connector.Connect(ctx, stale); err == nil || connects != 0 {
			t.Fatal("stale nondeleting snapshot reached upstream connect")
		}
	})
	t.Run("connected_before_hold", func(t *testing.T) {
		stale := makeObject()
		stale.DeletionTimestamp = nil
		kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stale).Build()
		observes, creates, updates, deletes := 0, 0, 0, 0
		innerClient := managed.ExternalClientFns{
			ObserveFn: func(context.Context, xpresource.Managed) (managed.ExternalObservation, error) {
				observes++
				return managed.ExternalObservation{}, nil
			},
			CreateFn: func(context.Context, xpresource.Managed) (managed.ExternalCreation, error) {
				creates++
				return managed.ExternalCreation{}, nil
			},
			UpdateFn: func(context.Context, xpresource.Managed) (managed.ExternalUpdate, error) {
				updates++
				return managed.ExternalUpdate{}, nil
			},
			DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
				deletes++
				return managed.ExternalDelete{}, nil
			},
		}
		inner := managed.ExternalConnectorFn(func(context.Context, xpresource.Managed) (managed.ExternalClient, error) { return innerClient, nil })
		connector := &deleteHoldConnector{inner: inner, reader: kube, writer: kube, enabled: false}
		external, err := connector.Connect(ctx, stale)
		if err != nil {
			t.Fatal(err)
		}
		if err := kube.Delete(ctx, stale.DeepCopy()); err != nil {
			t.Fatal(err)
		}
		current := &pscv1.ServiceConnectEndpoint{}
		if err := kube.Get(ctx, types.NamespacedName{Name: stale.Name}, current); err != nil {
			t.Fatal(err)
		}
		current.Annotations[deleteIntentAnnotation] = "held"
		if err := kube.Update(ctx, current); err != nil {
			t.Fatal(err)
		}
		if _, err := external.Observe(ctx, stale); err == nil {
			t.Fatal("stale observe was allowed")
		}
		if _, err := external.Create(ctx, stale); err == nil {
			t.Fatal("stale create was allowed")
		}
		if _, err := external.Update(ctx, stale); err == nil {
			t.Fatal("stale update was allowed")
		}
		if _, err := external.Delete(ctx, stale); err == nil {
			t.Fatal("flag-off delete was allowed")
		}
		if observes != 0 || creates != 0 || updates != 0 || deletes != 0 {
			t.Fatal("held resource reached upstream methods")
		}
	})
}

func TestPSCDeleteIdentity(t *testing.T) {
	now := metav1.Now()
	parent := "1"
	service := float64(7)
	obj := &pscv1.ServiceConnectEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "fixture", UID: "uid", DeletionTimestamp: &now, Finalizers: []string{managed.FinalizerName}}}
	obj.Spec.ProviderConfigReference = &xpv2.Reference{Name: "config"}
	obj.Spec.ForProvider.SubscriptionID = &parent
	obj.Spec.ForProvider.PrivateServiceConnectServiceID = &service
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"1/7/8", true}, {"+01/007/-8", true}, {"0/7/8", false}, {"1/8/8", false}, {"1/7", false}, {"1/7/no", false}, {"1/7/8/9", false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			item := obj.DeepCopy()
			meta.SetExternalName(item, tc.id)
			if got := sameDeleteIdentity(item, item); got != tc.want {
				t.Fatalf("identity accepted=%v want=%v", got, tc.want)
			}
		})
	}
	fractional := obj.DeepCopy()
	n := 7.5
	fractional.Spec.ForProvider.PrivateServiceConnectServiceID = &n
	meta.SetExternalName(fractional, "1/7/8")
	if sameDeleteIdentity(fractional, fractional) {
		t.Fatal("fractional service accepted")
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), 9007199254740992} {
		item := obj.DeepCopy()
		item.Spec.ForProvider.PrivateServiceConnectServiceID = &invalid
		meta.SetExternalName(item, "1/7/8")
		if sameDeleteIdentity(item, item) {
			t.Fatal("invalid service accepted")
		}
	}
	missing := obj.DeepCopy()
	missing.Spec.ForProvider.SubscriptionID = nil
	meta.SetExternalName(missing, "1/7/8")
	if sameDeleteIdentity(missing, missing) {
		t.Fatal("missing subscription accepted")
	}
	missing = obj.DeepCopy()
	missing.Spec.ForProvider.PrivateServiceConnectServiceID = nil
	meta.SetExternalName(missing, "1/7/8")
	if sameDeleteIdentity(missing, missing) {
		t.Fatal("missing service accepted")
	}
	missing = obj.DeepCopy()
	missing.Spec.ProviderConfigReference = nil
	meta.SetExternalName(missing, "1/7/8")
	if sameDeleteIdentity(missing, missing) {
		t.Fatal("missing provider accepted")
	}
	overflow := obj.DeepCopy()
	meta.SetExternalName(overflow, "1/7/999999999999999999999999999999")
	if sameDeleteIdentity(overflow, overflow) {
		t.Fatal("overflow endpoint accepted")
	}

}
