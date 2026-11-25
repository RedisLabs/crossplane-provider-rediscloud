// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"strconv"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"
	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/subscriptions"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
)

// ProSubscriptionReconciler reads identity and status for one existing Pro subscription.
type ProSubscriptionReconciler struct{ client.Client }

// SetupProSubscription adds the read-only Pro subscription lookup controller.
func SetupProSubscription(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-prosubscription", &lookupv1alpha1.ProSubscription{}, &ProSubscriptionReconciler{Client: mgr.GetClient()})
}

// Reconcile performs a single subscription GET; deletion has no Cloud side effect.
func (r *ProSubscriptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ProSubscription{}, req, proSubscriptionRef, proSubscriptionStatus, proSubscriptionAt, readProSubscription)
}

func proSubscriptionRef(obj *lookupv1alpha1.ProSubscription) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}

func proSubscriptionStatus(obj *lookupv1alpha1.ProSubscription) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}

func proSubscriptionAt(obj *lookupv1alpha1.ProSubscription) *lookupv1alpha1.ProSubscriptionObservation {
	return &obj.Status.AtProvider
}

func readProSubscription(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ProSubscription) (lookupv1alpha1.ProSubscriptionObservation, error) {
	var empty lookupv1alpha1.ProSubscriptionObservation
	id, err := strconv.Atoi(obj.Spec.ForProvider.SubscriptionID)
	if err != nil || id <= 0 {
		return empty, errors.New("subscriptionId must be a positive integer")
	}
	item, err := api.Subscription.Get(ctx, id)
	if err != nil {
		return empty, errors.Wrap(err, "cannot get Pro subscription")
	}
	if item == nil || item.ID == nil || *item.ID != id {
		return empty, errors.New("subscription response identity does not match the requested id")
	}
	if item.DeploymentType == nil || *item.DeploymentType != subscriptions.SubscriptionDeploymentTypeSingleRegion {
		return empty, errors.New("subscription is not a single-region Pro subscription")
	}
	return lookupv1alpha1.ProSubscriptionObservation{ID: strconv.Itoa(id), Name: redis.StringValue(item.Name), Status: redis.StringValue(item.Status), DeploymentType: *item.DeploymentType}, nil
}
