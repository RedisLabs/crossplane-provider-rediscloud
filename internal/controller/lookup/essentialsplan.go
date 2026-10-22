// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"strconv"
	"time"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"
	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/fixed/plans"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/internal/clients"
	"github.com/RedisLabs/provider-rediscloud/internal/lookup"
)

// EssentialsPlanReconciler looks up one Essentials plan.
type EssentialsPlanReconciler struct {
	client.Client
}

// SetupEssentialsPlan adds the Essentials plan lookup controller.
func SetupEssentialsPlan(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("lookup-essentialsplan").
		For(&lookupv1alpha1.EssentialsPlan{}).
		Complete(&EssentialsPlanReconciler{Client: mgr.GetClient()})
}

// Reconcile reads Essentials plans and writes the single match to status.
func (r *EssentialsPlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &lookupv1alpha1.EssentialsPlan{}
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
	list, err := listPlans(ctx, api, obj.Spec.ForProvider)
	if err != nil {
		return r.fail(ctx, obj, errors.Wrap(err, "cannot list Essentials plans"))
	}
	matched, err := lookup.MatchEssentialsPlan(list, planQuery(obj.Spec.ForProvider))
	if err != nil {
		return r.fail(ctx, obj, err)
	}
	obj.Status.AtProvider = observePlan(matched)
	obj.Status.SetConditions(xpv1.Available(), xpv1.ReconcileSuccess())
	return ctrl.Result{RequeueAfter: requeue}, r.Status().Update(ctx, obj)
}

func listPlans(ctx context.Context, api *rediscloud.Client, spec lookupv1alpha1.EssentialsPlanParameters) ([]*plans.GetPlanResponse, error) {
	if spec.SubscriptionID != nil && *spec.SubscriptionID != "" {
		id, err := strconv.Atoi(*spec.SubscriptionID)
		if err != nil {
			return nil, errors.Wrapf(err, "subscription id %q", *spec.SubscriptionID)
		}
		return api.FixedPlanSubscriptions.List(ctx, id)
	}
	if spec.CloudProvider != nil && *spec.CloudProvider != "" {
		return api.FixedPlans.ListWithProvider(ctx, lookup.CanonicalProvider(*spec.CloudProvider))
	}
	return api.FixedPlans.List(ctx)
}

func (r *EssentialsPlanReconciler) fail(ctx context.Context, obj *lookupv1alpha1.EssentialsPlan, err error) (ctrl.Result, error) {
	obj.Status.AtProvider = lookupv1alpha1.EssentialsPlanObservation{}
	reported := statusErr(err)
	obj.Status.SetConditions(xpv1.Unavailable().WithMessage(reported.Error()), xpv1.ReconcileError(reported))
	if updateErr := r.Status().Update(ctx, obj); updateErr != nil {
		return ctrl.Result{}, errors.Wrap(updateErr, reported.Error())
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func planQuery(spec lookupv1alpha1.EssentialsPlanParameters) lookup.EssentialsPlanQuery {
	return lookup.EssentialsPlanQuery{
		ID:                     deref(spec.ID),
		Name:                   deref(spec.Name),
		Size:                   deref(spec.Size),
		SizeMeasurementUnit:    deref(spec.SizeMeasurementUnit),
		CloudProvider:          lookup.CanonicalProvider(deref(spec.CloudProvider)),
		Region:                 deref(spec.Region),
		Availability:           deref(spec.Availability),
		SupportDataPersistence: spec.SupportDataPersistence,
		SupportReplication:     spec.SupportReplication,
	}
}

func observePlan(plan *plans.GetPlanResponse) lookupv1alpha1.EssentialsPlanObservation {
	size := ""
	if plan.Size != nil {
		size = strconv.FormatFloat(*plan.Size, 'f', -1, 64)
	}
	return lookupv1alpha1.EssentialsPlanObservation{
		ID:                  intString(plan.ID),
		Name:                redis.StringValue(plan.Name),
		Size:                size,
		SizeMeasurementUnit: redis.StringValue(plan.SizeMeasurementUnit),
		CloudProvider:       redis.StringValue(plan.Provider),
		Region:              redis.StringValue(plan.Region),
		RegionID:            intString(plan.RegionID),
		Availability:        redis.StringValue(plan.Availability),
		Price:               intString(plan.Price),
		PriceCurrency:       redis.StringValue(plan.PriceCurrency),
		PricePeriod:         redis.StringValue(plan.PricePeriod),
	}
}

func intString(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
