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

	"github.com/RedisLabs/rediscloud-go-api/redis"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/internal/clients"
	"github.com/RedisLabs/provider-rediscloud/internal/lookup"
)

const requeue = 10 * time.Minute

// PaymentMethodReconciler looks up one payment method.
type PaymentMethodReconciler struct {
	client.Client
}

// SetupPaymentMethod adds the payment method lookup controller.
func SetupPaymentMethod(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("lookup-paymentmethod").
		For(&lookupv1alpha1.PaymentMethod{}).
		Complete(&PaymentMethodReconciler{Client: mgr.GetClient()})
}

// Reconcile reads payment methods and writes the single match to status.
func (r *PaymentMethodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &lookupv1alpha1.PaymentMethod{}
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
	methods, err := api.Account.ListPaymentMethods(ctx)
	if err != nil {
		return r.fail(ctx, obj, errors.Wrap(err, "cannot list payment methods"))
	}
	query := lookup.PaymentMethodQuery{
		ExcludeExpired: true,
		Now:            time.Now(),
	}
	if obj.Spec.ForProvider.CardType != nil {
		query.CardType = *obj.Spec.ForProvider.CardType
	}
	if obj.Spec.ForProvider.LastFourNumbers != nil {
		query.LastFourNumbers = *obj.Spec.ForProvider.LastFourNumbers
	}
	if obj.Spec.ForProvider.ExcludeExpired != nil {
		query.ExcludeExpired = *obj.Spec.ForProvider.ExcludeExpired
	}
	matched, err := lookup.MatchPaymentMethod(methods, query)
	if err != nil {
		return r.fail(ctx, obj, err)
	}
	obj.Status.AtProvider = lookupv1alpha1.PaymentMethodObservation{
		ID:              strconv.Itoa(redis.IntValue(matched.ID)),
		CardType:        redis.StringValue(matched.Type),
		LastFourNumbers: lookup.LastFour(matched),
	}
	return r.ready(ctx, obj)
}

func (r *PaymentMethodReconciler) ready(ctx context.Context, obj *lookupv1alpha1.PaymentMethod) (ctrl.Result, error) {
	obj.Status.SetConditions(xpv1.Available(), xpv1.ReconcileSuccess())
	return ctrl.Result{RequeueAfter: requeue}, r.Status().Update(ctx, obj)
}

func (r *PaymentMethodReconciler) fail(ctx context.Context, obj *lookupv1alpha1.PaymentMethod, err error) (ctrl.Result, error) {
	obj.Status.AtProvider = lookupv1alpha1.PaymentMethodObservation{}
	reported := statusErr(err)
	obj.Status.SetConditions(xpv1.Unavailable().WithMessage(reported.Error()), xpv1.ReconcileError(reported))
	if updateErr := r.Status().Update(ctx, obj); updateErr != nil {
		return ctrl.Result{}, errors.Wrap(updateErr, reported.Error())
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}
