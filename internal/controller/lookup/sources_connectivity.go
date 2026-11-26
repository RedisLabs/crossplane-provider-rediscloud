// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"fmt"
	"strconv"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"
	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/privatelink"
	"github.com/RedisLabs/rediscloud-go-api/service/psc"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/internal/lookup"
)

// PrivateLinkReconciler looks up one Pro private link.
type PrivateLinkReconciler struct{ client.Client }

// SetupPrivateLink adds the private link lookup controller.
func SetupPrivateLink(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-privatelink", &lookupv1alpha1.PrivateLink{}, &PrivateLinkReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Pro private link.
func (r *PrivateLinkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.PrivateLink{}, req, privateLinkRef, privateLinkStatus, privateLinkAt, readPrivateLink)
}

func privateLinkRef(obj *lookupv1alpha1.PrivateLink) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func privateLinkStatus(obj *lookupv1alpha1.PrivateLink) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func privateLinkAt(obj *lookupv1alpha1.PrivateLink) *lookupv1alpha1.PrivateLinkObservation {
	return &obj.Status.AtProvider
}

func readPrivateLink(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.PrivateLink) (lookupv1alpha1.PrivateLinkObservation, error) {
	id, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.PrivateLinkObservation{}, err
	}
	item, err := api.PrivateLink.GetPrivateLink(ctx, id)
	if err != nil {
		return lookupv1alpha1.PrivateLinkObservation{}, errors.Wrap(err, "cannot get private link")
	}
	if item == nil {
		return lookupv1alpha1.PrivateLinkObservation{}, fmt.Errorf("private link was not found")
	}
	return lookup.ObservePrivateLink(item), nil
}

// ActiveActivePrivateLinkReconciler looks up one Active-Active private link.
type ActiveActivePrivateLinkReconciler struct{ client.Client }

// SetupActiveActivePrivateLink adds the Active-Active private link lookup controller.
func SetupActiveActivePrivateLink(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aaprivatelink", &lookupv1alpha1.ActiveActivePrivateLink{}, &ActiveActivePrivateLinkReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Active-Active private link.
func (r *ActiveActivePrivateLinkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ActiveActivePrivateLink{}, req, aaPrivateLinkRef, aaPrivateLinkStatus, aaPrivateLinkAt, readActiveActivePrivateLink)
}

func aaPrivateLinkRef(obj *lookupv1alpha1.ActiveActivePrivateLink) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func aaPrivateLinkStatus(obj *lookupv1alpha1.ActiveActivePrivateLink) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aaPrivateLinkAt(obj *lookupv1alpha1.ActiveActivePrivateLink) *lookupv1alpha1.PrivateLinkObservation {
	return &obj.Status.AtProvider
}

func readActiveActivePrivateLink(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ActiveActivePrivateLink) (lookupv1alpha1.PrivateLinkObservation, error) {
	subID, regionID, err := subscriptionRegion(obj.Spec.ForProvider)
	if err != nil {
		return lookupv1alpha1.PrivateLinkObservation{}, err
	}
	item, err := api.PrivateLink.GetActiveActivePrivateLink(ctx, subID, regionID)
	if err != nil {
		return lookupv1alpha1.PrivateLinkObservation{}, errors.Wrap(err, "cannot get active-active private link")
	}
	if item == nil {
		return lookupv1alpha1.PrivateLinkObservation{}, fmt.Errorf("private link was not found")
	}
	return lookup.ObservePrivateLink(item), nil
}

// PrivateLinkEndpointScriptReconciler looks up one Pro endpoint script.
type PrivateLinkEndpointScriptReconciler struct{ client.Client }

// SetupPrivateLinkEndpointScript adds the private link endpoint script controller.
func SetupPrivateLinkEndpointScript(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-privatelinkscript", &lookupv1alpha1.PrivateLinkEndpointScript{}, &PrivateLinkEndpointScriptReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Pro private link endpoint script.
func (r *PrivateLinkEndpointScriptReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.PrivateLinkEndpointScript{}, req, scriptRef, scriptStatus, scriptAt, readPrivateLinkEndpointScript)
}

func scriptRef(obj *lookupv1alpha1.PrivateLinkEndpointScript) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func scriptStatus(obj *lookupv1alpha1.PrivateLinkEndpointScript) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func scriptAt(obj *lookupv1alpha1.PrivateLinkEndpointScript) *lookupv1alpha1.PrivateLinkEndpointScriptObservation {
	return &obj.Status.AtProvider
}

func readPrivateLinkEndpointScript(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.PrivateLinkEndpointScript) (lookupv1alpha1.PrivateLinkEndpointScriptObservation, error) {
	id, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, err
	}
	subscription, err := api.Subscription.Get(ctx, id)
	if err != nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, errors.Wrap(err, "cannot get subscription")
	}
	if subscription == nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, fmt.Errorf("subscription was not found")
	}
	link, err := api.PrivateLink.GetPrivateLink(ctx, id)
	if err != nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, errors.Wrap(err, "cannot get private link")
	}
	if link == nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, fmt.Errorf("private link was not found")
	}
	script, err := api.PrivateLink.GetPrivateLinkEndpointScript(ctx, id)
	if err != nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, errors.Wrap(err, "cannot get private link endpoint script")
	}
	return lookupv1alpha1.PrivateLinkEndpointScriptObservation{
		ID:             strconv.Itoa(id),
		EndpointScript: scriptText(script),
	}, nil
}

// ActiveActivePrivateLinkEndpointScriptReconciler looks up one Active-Active script.
type ActiveActivePrivateLinkEndpointScriptReconciler struct{ client.Client }

// SetupActiveActivePrivateLinkEndpointScript adds the Active-Active script controller.
func SetupActiveActivePrivateLinkEndpointScript(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aaprivatelinkscript", &lookupv1alpha1.ActiveActivePrivateLinkEndpointScript{}, &ActiveActivePrivateLinkEndpointScriptReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Active-Active private link endpoint script.
func (r *ActiveActivePrivateLinkEndpointScriptReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ActiveActivePrivateLinkEndpointScript{}, req, aaScriptRef, aaScriptStatus, aaScriptAt, readActiveActivePrivateLinkEndpointScript)
}

func aaScriptRef(obj *lookupv1alpha1.ActiveActivePrivateLinkEndpointScript) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func aaScriptStatus(obj *lookupv1alpha1.ActiveActivePrivateLinkEndpointScript) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aaScriptAt(obj *lookupv1alpha1.ActiveActivePrivateLinkEndpointScript) *lookupv1alpha1.PrivateLinkEndpointScriptObservation {
	return &obj.Status.AtProvider
}

func readActiveActivePrivateLinkEndpointScript(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ActiveActivePrivateLinkEndpointScript) (lookupv1alpha1.PrivateLinkEndpointScriptObservation, error) {
	subID, regionID, err := subscriptionRegion(obj.Spec.ForProvider)
	if err != nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, err
	}
	script, err := api.PrivateLink.GetActiveActivePrivateLinkEndpointScript(ctx, subID, regionID)
	if err != nil {
		return lookupv1alpha1.PrivateLinkEndpointScriptObservation{}, errors.Wrap(err, "cannot get active-active private link endpoint script")
	}
	return lookupv1alpha1.PrivateLinkEndpointScriptObservation{
		ID:             strconv.Itoa(subID),
		EndpointScript: scriptText(script),
	}, nil
}

func scriptText(script *privatelink.PrivateLinkEndpointScript) string {
	if script == nil {
		return ""
	}
	return redis.StringValue(script.ResourceEndpointScript)
}

// PrivateServiceConnectReconciler looks up one Pro private service connect service.
type PrivateServiceConnectReconciler struct{ client.Client }

// SetupPrivateServiceConnect adds the private service connect lookup controller.
func SetupPrivateServiceConnect(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-psc", &lookupv1alpha1.PrivateServiceConnect{}, &PrivateServiceConnectReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Pro private service connect service.
func (r *PrivateServiceConnectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.PrivateServiceConnect{}, req, pscRef, pscStatus, pscAt, readPrivateServiceConnect)
}

func pscRef(obj *lookupv1alpha1.PrivateServiceConnect) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func pscStatus(obj *lookupv1alpha1.PrivateServiceConnect) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func pscAt(obj *lookupv1alpha1.PrivateServiceConnect) *lookupv1alpha1.PrivateServiceConnectObservation {
	return &obj.Status.AtProvider
}

func readPrivateServiceConnect(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.PrivateServiceConnect) (lookupv1alpha1.PrivateServiceConnectObservation, error) {
	id, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectObservation{}, err
	}
	item, err := api.PrivateServiceConnect.GetService(ctx, id)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectObservation{}, errors.Wrap(err, "cannot get private service connect")
	}
	return lookup.ObservePrivateServiceConnect(id, item)
}

// ActiveActivePrivateServiceConnectReconciler looks up one Active-Active service.
type ActiveActivePrivateServiceConnectReconciler struct{ client.Client }

// SetupActiveActivePrivateServiceConnect adds the Active-Active service controller.
func SetupActiveActivePrivateServiceConnect(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aapsc", &lookupv1alpha1.ActiveActivePrivateServiceConnect{}, &ActiveActivePrivateServiceConnectReconciler{Client: mgr.GetClient()})
}

// Reconcile reads one Active-Active private service connect service.
func (r *ActiveActivePrivateServiceConnectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ActiveActivePrivateServiceConnect{}, req, aaPSCRef, aaPSCStatus, aaPSCAt, readActiveActivePrivateServiceConnect)
}

func aaPSCRef(obj *lookupv1alpha1.ActiveActivePrivateServiceConnect) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func aaPSCStatus(obj *lookupv1alpha1.ActiveActivePrivateServiceConnect) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aaPSCAt(obj *lookupv1alpha1.ActiveActivePrivateServiceConnect) *lookupv1alpha1.PrivateServiceConnectObservation {
	return &obj.Status.AtProvider
}

func readActiveActivePrivateServiceConnect(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ActiveActivePrivateServiceConnect) (lookupv1alpha1.PrivateServiceConnectObservation, error) {
	subID, regionID, err := subscriptionRegion(obj.Spec.ForProvider)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectObservation{}, err
	}
	item, err := api.PrivateServiceConnect.GetActiveActiveService(ctx, subID, regionID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectObservation{}, errors.Wrap(err, "cannot get active-active private service connect")
	}
	return lookup.ObservePrivateServiceConnect(subID, item)
}

// PrivateServiceConnectEndpointsReconciler looks up Pro private service connect endpoints.
type PrivateServiceConnectEndpointsReconciler struct{ client.Client }

// SetupPrivateServiceConnectEndpoints adds the endpoint lookup controller.
func SetupPrivateServiceConnectEndpoints(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-pscendpoints", &lookupv1alpha1.PrivateServiceConnectEndpoints{}, &PrivateServiceConnectEndpointsReconciler{Client: mgr.GetClient()})
}

// Reconcile reads Pro private service connect endpoints.
func (r *PrivateServiceConnectEndpointsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.PrivateServiceConnectEndpoints{}, req, pscEndpointsRef, pscEndpointsStatus, pscEndpointsAt, readPrivateServiceConnectEndpoints)
}

func pscEndpointsRef(obj *lookupv1alpha1.PrivateServiceConnectEndpoints) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func pscEndpointsStatus(obj *lookupv1alpha1.PrivateServiceConnectEndpoints) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func pscEndpointsAt(obj *lookupv1alpha1.PrivateServiceConnectEndpoints) *lookupv1alpha1.PrivateServiceConnectEndpointsObservation {
	return &obj.Status.AtProvider
}

func readPrivateServiceConnectEndpoints(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.PrivateServiceConnectEndpoints) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, error) {
	subID, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, err
	}
	serviceID, err := lookup.ParseID("private service connect service id", obj.Spec.ForProvider.PrivateServiceConnectServiceID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, err
	}
	got, err := api.PrivateServiceConnect.GetEndpoints(ctx, subID, serviceID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, errors.Wrap(err, "cannot get private service connect endpoints")
	}
	if identityErr := validatePSCServiceIdentity(got, serviceID); identityErr != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, identityErr
	}
	endpoints, err := observeEndpoints(got, func(endpointID int) (*psc.CreationScript, error) {
		return api.PrivateServiceConnect.GetEndpointCreationScripts(ctx, subID, serviceID, endpointID, lookup.IncludeTerraformGCPScript)
	})
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, err
	}
	return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{
		ID:        fmt.Sprintf("%d/%d", subID, serviceID),
		Endpoints: endpoints,
	}, nil
}

// ActiveActivePrivateServiceConnectEndpointsReconciler looks up Active-Active endpoints.
type ActiveActivePrivateServiceConnectEndpointsReconciler struct{ client.Client }

// SetupActiveActivePrivateServiceConnectEndpoints adds the Active-Active endpoint controller.
func SetupActiveActivePrivateServiceConnectEndpoints(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aapscendpoints", &lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints{}, &ActiveActivePrivateServiceConnectEndpointsReconciler{Client: mgr.GetClient()})
}

// Reconcile reads Active-Active private service connect endpoints.
func (r *ActiveActivePrivateServiceConnectEndpointsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints{}, req, aaPSCEndpointsRef, aaPSCEndpointsStatus, aaPSCEndpointsAt, readActiveActivePrivateServiceConnectEndpoints)
}

func aaPSCEndpointsRef(obj *lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func aaPSCEndpointsStatus(obj *lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aaPSCEndpointsAt(obj *lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints) *lookupv1alpha1.PrivateServiceConnectEndpointsObservation {
	return &obj.Status.AtProvider
}

func readActiveActivePrivateServiceConnectEndpoints(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ActiveActivePrivateServiceConnectEndpoints) (lookupv1alpha1.PrivateServiceConnectEndpointsObservation, error) {
	subID, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, err
	}
	regionID, err := lookup.ParseID("region id", obj.Spec.ForProvider.RegionID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, err
	}
	serviceID, err := lookup.ParseID("private service connect service id", obj.Spec.ForProvider.PrivateServiceConnectServiceID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, err
	}
	got, err := api.PrivateServiceConnect.GetActiveActiveEndpoints(ctx, subID, regionID, serviceID)
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, errors.Wrap(err, "cannot get active-active private service connect endpoints")
	}
	if identityErr := validatePSCServiceIdentity(got, serviceID); identityErr != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, identityErr
	}
	endpoints, err := observeEndpoints(got, func(endpointID int) (*psc.CreationScript, error) {
		return api.PrivateServiceConnect.GetActiveActiveEndpointCreationScripts(ctx, subID, regionID, serviceID, endpointID, lookup.IncludeTerraformGCPScript)
	})
	if err != nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{}, err
	}
	return lookupv1alpha1.PrivateServiceConnectEndpointsObservation{
		ID:        fmt.Sprintf("%d/%d", subID, serviceID),
		Endpoints: endpoints,
	}, nil
}

func validatePSCServiceIdentity(got *psc.PrivateServiceConnectEndpoints, requested int) error {
	if got != nil && got.PSCServiceID != nil && *got.PSCServiceID != requested {
		return fmt.Errorf("private service connect response identity does not match request")
	}
	return nil
}

func observeEndpoints(got *psc.PrivateServiceConnectEndpoints, script func(int) (*psc.CreationScript, error)) ([]lookupv1alpha1.PrivateServiceConnectEndpointObservation, error) {
	if got == nil {
		return nil, fmt.Errorf("private service connect endpoints response is missing")
	}
	out := make([]lookupv1alpha1.PrivateServiceConnectEndpointObservation, 0, len(got.Endpoints))
	for _, endpoint := range got.Endpoints {
		if endpoint == nil {
			continue
		}
		var attachments []lookupv1alpha1.ServiceAttachmentObservation
		if lookup.EndpointNeedsScript(redis.StringValue(endpoint.Status)) {
			if endpoint.ID == nil {
				return nil, fmt.Errorf("private service connect endpoint id is missing")
			}
			creation, err := script(redis.IntValue(endpoint.ID))
			if err != nil {
				return nil, errors.Wrap(err, "cannot get private service connect creation script")
			}
			attachments, err = lookup.ObserveServiceAttachments(creation)
			if err != nil {
				return nil, err
			}
		}
		observed, err := lookup.ObserveEndpoint(endpoint, attachments)
		if err != nil {
			return nil, err
		}
		out = append(out, observed)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SubscriptionPeeringsReconciler looks up VPC peerings.
type SubscriptionPeeringsReconciler struct{ client.Client }

// SetupSubscriptionPeerings adds the peering lookup controller.
func SetupSubscriptionPeerings(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-peerings", &lookupv1alpha1.SubscriptionPeerings{}, &SubscriptionPeeringsReconciler{Client: mgr.GetClient()})
}

// Reconcile reads VPC peerings for one subscription.
func (r *SubscriptionPeeringsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.SubscriptionPeerings{}, req, peeringsRef, peeringsStatus, peeringsAt, readSubscriptionPeerings)
}

func peeringsRef(obj *lookupv1alpha1.SubscriptionPeerings) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func peeringsStatus(obj *lookupv1alpha1.SubscriptionPeerings) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func peeringsAt(obj *lookupv1alpha1.SubscriptionPeerings) *lookupv1alpha1.SubscriptionPeeringsObservation {
	return &obj.Status.AtProvider
}

func readSubscriptionPeerings(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.SubscriptionPeerings) (lookupv1alpha1.SubscriptionPeeringsObservation, error) {
	id, err := lookup.ParseID("subscription id", obj.Spec.ForProvider.SubscriptionID)
	if err != nil {
		return lookupv1alpha1.SubscriptionPeeringsObservation{}, err
	}
	items, err := api.Subscription.ListVPCPeering(ctx, id)
	if err != nil {
		return lookupv1alpha1.SubscriptionPeeringsObservation{}, errors.Wrap(err, "cannot list subscription peerings")
	}
	return lookup.FilterPeerings(items, deref(obj.Spec.ForProvider.Status)), nil
}

func subscriptionRegion(spec lookupv1alpha1.SubscriptionRegionParameters) (int, int, error) {
	subID, err := lookup.ParseID("subscription id", spec.SubscriptionID)
	if err != nil {
		return 0, 0, err
	}
	regionID, err := lookup.ParseID("region id", spec.RegionID)
	if err != nil {
		return 0, 0, err
	}
	return subID, regionID, nil
}
