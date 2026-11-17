// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"
	"github.com/RedisLabs/rediscloud-go-api/redis"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/internal/lookup"
)

// TransitGatewayReconciler looks up one Pro transit gateway attachment.
type TransitGatewayReconciler struct{ client.Client }

// SetupTransitGateway adds the transit gateway lookup controller.
func SetupTransitGateway(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-transitgateway", &lookupv1alpha1.TransitGateway{}, &TransitGatewayReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Pro transit gateway attachment.
func (r *TransitGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.TransitGateway{}, req, transitRef, transitStatus, transitAt, readTransitGateway)
}

func transitRef(obj *lookupv1alpha1.TransitGateway) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func transitStatus(obj *lookupv1alpha1.TransitGateway) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func transitAt(obj *lookupv1alpha1.TransitGateway) *lookupv1alpha1.TransitGatewayObservation {
	return &obj.Status.AtProvider
}

func readTransitGateway(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.TransitGateway) (lookupv1alpha1.TransitGatewayObservation, error) {
	id, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.TransitGatewayObservation{}, err
	}
	task, err := api.TransitGatewayAttachments.Get(ctx, id)
	if err != nil {
		return lookupv1alpha1.TransitGatewayObservation{}, errors.Wrap(err, "cannot get transit gateway")
	}
	matched, err := lookup.MatchTransitGateway(task, lookup.TransitGatewayQuery{
		TGWID:     deref(obj.Spec.ForProvider.TGWID),
		AWSTGWUID: deref(obj.Spec.ForProvider.AWSTGWUID),
	})
	if err != nil {
		return lookupv1alpha1.TransitGatewayObservation{}, err
	}
	return lookup.ObserveTransitGateway(id, matched), nil
}

// ActiveActiveTransitGatewayReconciler looks up one Active-Active attachment.
type ActiveActiveTransitGatewayReconciler struct{ client.Client }

// SetupActiveActiveTransitGateway adds the Active-Active transit gateway controller.
func SetupActiveActiveTransitGateway(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aatransitgateway", &lookupv1alpha1.ActiveActiveTransitGateway{}, &ActiveActiveTransitGatewayReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Active-Active transit gateway attachment.
func (r *ActiveActiveTransitGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ActiveActiveTransitGateway{}, req, aaTransitRef, aaTransitStatus, aaTransitAt, readActiveActiveTransitGateway)
}

func aaTransitRef(obj *lookupv1alpha1.ActiveActiveTransitGateway) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func aaTransitStatus(obj *lookupv1alpha1.ActiveActiveTransitGateway) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aaTransitAt(obj *lookupv1alpha1.ActiveActiveTransitGateway) *lookupv1alpha1.TransitGatewayObservation {
	return &obj.Status.AtProvider
}

func readActiveActiveTransitGateway(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ActiveActiveTransitGateway) (lookupv1alpha1.TransitGatewayObservation, error) {
	subID, regionID, err := subscriptionRegion(lookupv1alpha1.SubscriptionRegionParameters{
		SubscriptionID: obj.Spec.ForProvider.SubscriptionID,
		RegionID:       obj.Spec.ForProvider.RegionID,
	})
	if err != nil {
		return lookupv1alpha1.TransitGatewayObservation{}, err
	}
	task, err := api.TransitGatewayAttachments.GetActiveActive(ctx, subID, regionID)
	if err != nil {
		return lookupv1alpha1.TransitGatewayObservation{}, errors.Wrap(err, "cannot get active-active transit gateway")
	}
	matched, err := lookup.MatchTransitGateway(task, lookup.TransitGatewayQuery{
		TGWID:     deref(obj.Spec.ForProvider.TGWID),
		AWSTGWUID: deref(obj.Spec.ForProvider.AWSTGWUID),
	})
	if err != nil {
		return lookupv1alpha1.TransitGatewayObservation{}, err
	}
	return lookup.ObserveTransitGateway(subID, matched), nil
}

// TransitGatewayInvitationsReconciler looks up Pro transit gateway invitations.
type TransitGatewayInvitationsReconciler struct{ client.Client }

// SetupTransitGatewayInvitations adds the invitation lookup controller.
func SetupTransitGatewayInvitations(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-tgwinvitations", &lookupv1alpha1.TransitGatewayInvitations{}, &TransitGatewayInvitationsReconciler{Client: mgr.GetClient()})
}

// Reconcile reads Pro transit gateway invitations.
func (r *TransitGatewayInvitationsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.TransitGatewayInvitations{}, req, invitationsRef, invitationsStatus, invitationsAt, readTransitGatewayInvitations)
}

func invitationsRef(obj *lookupv1alpha1.TransitGatewayInvitations) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func invitationsStatus(obj *lookupv1alpha1.TransitGatewayInvitations) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func invitationsAt(obj *lookupv1alpha1.TransitGatewayInvitations) *lookupv1alpha1.TransitGatewayInvitationsObservation {
	return &obj.Status.AtProvider
}

func readTransitGatewayInvitations(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.TransitGatewayInvitations) (lookupv1alpha1.TransitGatewayInvitationsObservation, error) {
	id, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.TransitGatewayInvitationsObservation{}, err
	}
	items, err := api.TransitGatewayAttachments.ListInvitations(ctx, id)
	if err != nil {
		return lookupv1alpha1.TransitGatewayInvitationsObservation{}, errors.Wrap(err, "cannot list transit gateway invitations")
	}
	return lookup.ObserveInvitations(id, items), nil
}

// ActiveActiveTransitGatewayInvitationsReconciler looks up Active-Active invitations.
type ActiveActiveTransitGatewayInvitationsReconciler struct{ client.Client }

// SetupActiveActiveTransitGatewayInvitations adds the Active-Active invitation controller.
func SetupActiveActiveTransitGatewayInvitations(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aatgwinvitations", &lookupv1alpha1.ActiveActiveTransitGatewayInvitations{}, &ActiveActiveTransitGatewayInvitationsReconciler{Client: mgr.GetClient()})
}

// Reconcile reads Active-Active transit gateway invitations.
func (r *ActiveActiveTransitGatewayInvitationsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ActiveActiveTransitGatewayInvitations{}, req, aaInvitationsRef, aaInvitationsStatus, aaInvitationsAt, readActiveActiveTransitGatewayInvitations)
}

func aaInvitationsRef(obj *lookupv1alpha1.ActiveActiveTransitGatewayInvitations) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func aaInvitationsStatus(obj *lookupv1alpha1.ActiveActiveTransitGatewayInvitations) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aaInvitationsAt(obj *lookupv1alpha1.ActiveActiveTransitGatewayInvitations) *lookupv1alpha1.TransitGatewayInvitationsObservation {
	return &obj.Status.AtProvider
}

func readActiveActiveTransitGatewayInvitations(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ActiveActiveTransitGatewayInvitations) (lookupv1alpha1.TransitGatewayInvitationsObservation, error) {
	subID, regionID, err := subscriptionRegion(obj.Spec.ForProvider)
	if err != nil {
		return lookupv1alpha1.TransitGatewayInvitationsObservation{}, err
	}
	items, err := api.TransitGatewayAttachments.ListInvitationsActiveActive(ctx, subID, regionID)
	if err != nil {
		return lookupv1alpha1.TransitGatewayInvitationsObservation{}, errors.Wrap(err, "cannot list active-active transit gateway invitations")
	}
	return lookup.ObserveInvitations(subID, items), nil
}

// ActiveActiveSubscriptionRegionsReconciler looks up regions on one Active-Active subscription.
type ActiveActiveSubscriptionRegionsReconciler struct{ client.Client }

// SetupActiveActiveSubscriptionRegions adds the Active-Active region lookup controller.
func SetupActiveActiveSubscriptionRegions(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aaregions", &lookupv1alpha1.ActiveActiveSubscriptionRegions{}, &ActiveActiveSubscriptionRegionsReconciler{Client: mgr.GetClient()})
}

// Reconcile reads regions for one named Active-Active subscription.
func (r *ActiveActiveSubscriptionRegionsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ActiveActiveSubscriptionRegions{}, req, aaRegionsRef, aaRegionsStatus, aaRegionsAt, readActiveActiveSubscriptionRegions)
}

func aaRegionsRef(obj *lookupv1alpha1.ActiveActiveSubscriptionRegions) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func aaRegionsStatus(obj *lookupv1alpha1.ActiveActiveSubscriptionRegions) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aaRegionsAt(obj *lookupv1alpha1.ActiveActiveSubscriptionRegions) *lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation {
	return &obj.Status.AtProvider
}

func readActiveActiveSubscriptionRegions(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ActiveActiveSubscriptionRegions) (lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation, error) {
	items, err := api.Subscription.List(ctx)
	if err != nil {
		return lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation{}, errors.Wrap(err, "cannot list subscriptions")
	}
	matched, err := lookup.MatchActiveActiveSubscription(items, obj.Spec.ForProvider.SubscriptionName)
	if err != nil {
		return lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation{}, err
	}
	if matched.ID == nil {
		return lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation{}, fmt.Errorf("active-active subscription id is missing")
	}
	regions, err := api.Subscription.ListActiveActiveRegions(ctx, redis.IntValue(matched.ID))
	if err != nil {
		return lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation{}, errors.Wrap(err, "cannot list active-active regions")
	}
	observed := lookup.ObserveActiveActiveRegions(redis.IntValue(matched.ID), regions)
	if len(observed.Regions) == 0 {
		return lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation{}, lookup.ErrNone
	}
	return observed, nil
}
