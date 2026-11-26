// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package serviceconnectendpoint

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	pscv1 "github.com/RedisLabs/provider-rediscloud/apis/private/v1beta1"
	"github.com/RedisLabs/provider-rediscloud/internal/features"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	ujcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deleteIntentAnnotation is a durable hold, not an authenticated marker. The
// opt-in mode requires trusted CR/ProviderConfig/Secret writers and a drained
// rollout. See docs/design/psc-endpoint-delete-conservative-hold.md.
const deleteIntentAnnotation = "rediscloud.crossplane.io/psc-endpoint-delete-intent"

var errDeleteHeld = errors.New("PSC endpoint deletion is paused because safety is unconfirmed; preserve the resource and contact an administrator")

type deleteIntent struct {
	Version  int    `json:"version"`
	UID      string `json:"uid"`
	External string `json:"external"`
	Provider string `json:"provider"`
	Token    string `json:"token"`
}
type deleteHoldConnector struct {
	inner   managed.ExternalConnector
	reader  client.Reader
	writer  client.Client
	enabled bool
}

func protectPSCDelete(mgr ctrl.Manager, o ujcontroller.Options, inner managed.ExternalConnector) managed.ExternalConnector {
	return NewDeleteHoldConnector(mgr.GetAPIReader(), mgr.GetClient(), inner, o.Features != nil && o.Features.Enabled(features.EnablePSCEndpointDeleteHold))
}

// NewDeleteHoldConnector constructs the controller's guard. The reader must
// be an uncached API reader; it is exported for hosted integration tests.
func NewDeleteHoldConnector(reader client.Reader, writer client.Client, inner managed.ExternalConnector, enabled bool) managed.ExternalConnector {
	return &deleteHoldConnector{inner: inner, reader: reader, writer: writer, enabled: enabled}
}
func (c *deleteHoldConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	obj, ok := mg.(*pscv1.ServiceConnectEndpoint)
	if !ok || checkCurrent(ctx, c.reader, obj) != nil {
		return nil, errDeleteHeld
	}
	inner, err := c.inner.Connect(ctx, mg)
	if err != nil {
		return nil, err
	}
	return &deleteHoldExternal{ExternalClient: inner, reader: c.reader, writer: c.writer, enabled: c.enabled}, nil
}

type deleteHoldExternal struct {
	managed.ExternalClient
	reader  client.Reader
	writer  client.Client
	enabled bool
}

// Existing intent always holds, including after the opt-in flag is disabled.
func (e *deleteHoldExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	obj, ok := mg.(*pscv1.ServiceConnectEndpoint)
	if !ok {
		return managed.ExternalObservation{}, errDeleteHeld
	}
	if err := checkCurrent(ctx, e.reader, obj); err != nil {
		return managed.ExternalObservation{}, errDeleteHeld
	}
	return e.ExternalClient.Observe(ctx, mg)
}

// Authoritative reads prevent a stale nondeleting cache snapshot from
// starting another external operation after the server has recorded intent.
func checkCurrent(ctx context.Context, reader client.Reader, snapshot *pscv1.ServiceConnectEndpoint) error {
	if snapshot == nil || snapshot.UID == "" {
		return errDeleteHeld
	}
	current := &pscv1.ServiceConnectEndpoint{}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(snapshot), current); err != nil || current.UID != snapshot.UID {
		return errDeleteHeld
	}
	if _, held := current.Annotations[deleteIntentAnnotation]; held {
		return errDeleteHeld
	}
	if current.GetDeletionTimestamp().IsZero() != snapshot.GetDeletionTimestamp().IsZero() {
		return errDeleteHeld
	}
	return nil
}

func (e *deleteHoldExternal) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	obj, ok := mg.(*pscv1.ServiceConnectEndpoint)
	if !ok || !obj.GetDeletionTimestamp().IsZero() || checkCurrent(ctx, e.reader, obj) != nil {
		return managed.ExternalCreation{}, errDeleteHeld
	}
	return e.ExternalClient.Create(ctx, mg)
}

func (e *deleteHoldExternal) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	obj, ok := mg.(*pscv1.ServiceConnectEndpoint)
	if !ok || !obj.GetDeletionTimestamp().IsZero() || checkCurrent(ctx, e.reader, obj) != nil {
		return managed.ExternalUpdate{}, errDeleteHeld
	}
	return e.ExternalClient.Update(ctx, mg)
}

func (e *deleteHoldExternal) Delete(ctx context.Context, mg xpresource.Managed) (managed.ExternalDelete, error) {
	obj, ok := mg.(*pscv1.ServiceConnectEndpoint)
	if !ok {
		return managed.ExternalDelete{}, errDeleteHeld
	}
	current := &pscv1.ServiceConnectEndpoint{}
	if err := e.reader.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil || current.UID != obj.UID {
		return managed.ExternalDelete{}, errDeleteHeld
	}
	if _, held := current.Annotations[deleteIntentAnnotation]; held {
		return managed.ExternalDelete{}, errDeleteHeld
	}
	if !e.enabled {
		return e.ExternalClient.Delete(ctx, mg)
	}
	raw, err := reserveDeleteIntent(ctx, e.reader, e.writer, obj)
	if err != nil {
		return managed.ExternalDelete{}, errDeleteHeld
	}
	fresh := &pscv1.ServiceConnectEndpoint{}
	if err := e.reader.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil || !sameDeleteIdentity(fresh, obj) || fresh.Annotations[deleteIntentAnnotation] != raw {
		return managed.ExternalDelete{}, errDeleteHeld
	}
	obj.ResourceVersion = fresh.ResourceVersion
	return e.ExternalClient.Delete(ctx, mg)
}

func reserveDeleteIntent(ctx context.Context, reader client.Reader, writer client.Client, snapshot *pscv1.ServiceConnectEndpoint) (string, error) { //nolint:gocyclo // Reservation validates each independent safety condition before dispatch.
	if !sameDeleteIdentity(snapshot, snapshot) {
		return "", errDeleteHeld
	}
	current := &pscv1.ServiceConnectEndpoint{}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(snapshot), current); err != nil || !sameDeleteIdentity(current, snapshot) || current.ResourceVersion == "" {
		return "", errDeleteHeld
	}
	if _, held := current.Annotations[deleteIntentAnnotation]; held {
		return "", errDeleteHeld
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errDeleteHeld
	}
	data, err := json.Marshal(deleteIntent{Version: 1, UID: string(current.UID), External: meta.GetExternalName(current), Provider: current.Spec.ProviderConfigReference.Name, Token: hex.EncodeToString(nonce[:])})
	if err != nil {
		return "", errDeleteHeld
	}
	annotations := current.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[deleteIntentAnnotation] = string(data)
	current.SetAnnotations(annotations)
	if err := writer.Update(ctx, current); err != nil {
		return "", errDeleteHeld
	}
	if !sameDeleteIdentity(current, snapshot) || current.Annotations[deleteIntentAnnotation] != string(data) {
		return "", errDeleteHeld
	}
	return string(data), nil
}

// Parse the exact provider ID grammar (three strconv.Atoi components) without
// inventing positivity or canonical-text constraints. The declaration must
// resolve to the same numeric parent and service; floats must be exact.
func sameDeleteIdentity(current, snapshot *pscv1.ServiceConnectEndpoint) bool { //nolint:gocyclo // Every independent identity and lifecycle guard is required.
	if current == nil || snapshot == nil || current.UID == "" || current.UID != snapshot.UID || current.GetDeletionTimestamp().IsZero() || !slices.Contains(current.Finalizers, managed.FinalizerName) || !reflect.DeepEqual(current.Spec, snapshot.Spec) || current.Spec.ProviderConfigReference == nil || current.Spec.ProviderConfigReference.Name == "" || current.Spec.ForProvider.SubscriptionID == nil || current.Spec.ForProvider.PrivateServiceConnectServiceID == nil {
		return false
	}
	external := meta.GetExternalName(current)
	if external != meta.GetExternalName(snapshot) {
		return false
	}
	parts := strings.Split(external, "/")
	if len(parts) != 3 {
		return false
	}
	sub, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	service, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	if _, err := strconv.Atoi(parts[2]); err != nil {
		return false
	}
	declaredSub, err := strconv.Atoi(*current.Spec.ForProvider.SubscriptionID)
	if err != nil || declaredSub != sub {
		return false
	}
	declaredService := *current.Spec.ForProvider.PrivateServiceConnectServiceID
	return !math.IsNaN(declaredService) && !math.IsInf(declaredService, 0) && math.Abs(declaredService) <= 9007199254740991 && math.Trunc(declaredService) == declaredService && float64(service) == declaredService
}
