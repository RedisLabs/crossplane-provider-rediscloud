// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"time"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/internal/clients"
	"github.com/RedisLabs/provider-rediscloud/internal/lookup"
)

// RegionsReconciler looks up Redis Cloud regions.
type RegionsReconciler struct {
	client.Client
}

// SetupRegions adds the region lookup controller.
func SetupRegions(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("lookup-regions").
		For(&lookupv1alpha1.Regions{}).
		Complete(&RegionsReconciler{Client: mgr.GetClient()})
}

// Reconcile reads regions and writes the matches to status.
func (r *RegionsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &lookupv1alpha1.Regions{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	creds, err := clients.LoadCredentials(ctx, r.Client, obj.Spec.ProviderConfigRef)
	if err != nil {
		return r.fail(ctx, obj, err)
	}
	api, err := newAPI(creds)
	if err != nil {
		return r.fail(ctx, obj, err)
	}
	regions, err := api.Account.ListRegions(ctx)
	if err != nil {
		return r.fail(ctx, obj, errors.Wrap(err, "cannot list regions"))
	}
	matched, err := lookup.MatchRegions(regions, lookup.RegionQuery{
		ProviderName: deref(obj.Spec.ForProvider.ProviderName),
		Name:         deref(obj.Spec.ForProvider.Name),
	})
	if err != nil {
		return r.fail(ctx, obj, err)
	}
	observed := make([]lookupv1alpha1.RegionObservation, 0, len(matched))
	for _, region := range matched {
		observed = append(observed, lookupv1alpha1.RegionObservation{
			RegionID:     intString(region.ID),
			Name:         redis.StringValue(region.Name),
			ProviderName: redis.StringValue(region.Provider),
		})
	}
	obj.Status.AtProvider.Regions = observed
	obj.Status.SetConditions(xpv1.Available(), xpv1.ReconcileSuccess())
	return ctrl.Result{RequeueAfter: requeue}, r.Status().Update(ctx, obj)
}

func (r *RegionsReconciler) fail(ctx context.Context, obj *lookupv1alpha1.Regions, err error) (ctrl.Result, error) {
	obj.Status.AtProvider.Regions = nil
	reported := statusErr(err)
	obj.Status.SetConditions(xpv1.Unavailable().WithMessage(reported.Error()), xpv1.ReconcileError(reported))
	if updateErr := r.Status().Update(ctx, obj); updateErr != nil {
		return ctrl.Result{}, errors.Wrap(updateErr, reported.Error())
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}
