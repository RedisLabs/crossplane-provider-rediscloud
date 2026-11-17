// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	"github.com/RedisLabs/provider-rediscloud/internal/clients"
)

// runLookup loads one lookup, calls the Redis Cloud API, and writes status.
// Delete is a no-op: these objects have no finalizer and change nothing in
// the account.
func runLookup[T client.Object, O any](
	ctx context.Context,
	kube client.Client,
	obj T,
	req ctrl.Request,
	ref func(T) xpv1.Reference,
	status func(T) *xpv1.ManagedResourceStatus,
	at func(T) *O,
	read func(context.Context, *rediscloud.Client, T) (O, error),
) (ctrl.Result, error) {
	if err := kube.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	var observed O
	api, err := openClient(ctx, kube, ref(obj))
	if err == nil {
		observed, err = read(ctx, api, obj)
	}
	return settle(ctx, kube, obj, observed, err, status(obj), at(obj))
}

func openClient(ctx context.Context, kube client.Client, ref xpv1.Reference) (*rediscloud.Client, error) {
	creds, err := clients.LoadCredentials(ctx, kube, ref)
	if err != nil {
		return nil, err
	}
	return newAPI(creds)
}

func settle[O any](ctx context.Context, kube client.Client, obj client.Object, observed O, failed error, status *xpv1.ManagedResourceStatus, at *O) (ctrl.Result, error) {
	if failed != nil {
		var zero O
		*at = zero
		reported := statusErr(failed)
		status.SetConditions(xpv1.Unavailable().WithMessage(reported.Error()), xpv1.ReconcileError(reported))
		if updateErr := kube.Status().Update(ctx, obj); updateErr != nil {
			return ctrl.Result{}, errors.Wrap(updateErr, reported.Error())
		}
		return ctrl.Result{RequeueAfter: requeueFailed}, nil
	}
	*at = observed
	status.SetConditions(xpv1.Available(), xpv1.ReconcileSuccess())
	return ctrl.Result{RequeueAfter: requeue}, kube.Status().Update(ctx, obj)
}
