// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// SetupFunc registers one lookup controller.
type SetupFunc struct {
	Kind string
	Fn   func(ctrl.Manager) error
}

// Setups returns every lookup controller in registration order.
func Setups() []SetupFunc {
	return []SetupFunc{
		{Kind: "PaymentMethod", Fn: SetupPaymentMethod},
		{Kind: "EssentialsPlan", Fn: SetupEssentialsPlan},
		{Kind: "Regions", Fn: SetupRegions},
		{Kind: "ACLRole", Fn: SetupACLRole},
		{Kind: "ACLRule", Fn: SetupACLRule},
		{Kind: "ACLUser", Fn: SetupACLUser},
		{Kind: "CloudAccount", Fn: SetupCloudAccount},
		{Kind: "DataPersistence", Fn: SetupDataPersistence},
		{Kind: "DatabaseModules", Fn: SetupDatabaseModules},
		{Kind: "EssentialsSubscription", Fn: SetupEssentialsSubscription},
		{Kind: "PrivateLink", Fn: SetupPrivateLink},
		{Kind: "ActiveActivePrivateLink", Fn: SetupActiveActivePrivateLink},
		{Kind: "PrivateLinkEndpointScript", Fn: SetupPrivateLinkEndpointScript},
		{Kind: "ActiveActivePrivateLinkEndpointScript", Fn: SetupActiveActivePrivateLinkEndpointScript},
		{Kind: "PrivateServiceConnect", Fn: SetupPrivateServiceConnect},
		{Kind: "ActiveActivePrivateServiceConnect", Fn: SetupActiveActivePrivateServiceConnect},
		{Kind: "PrivateServiceConnectEndpoints", Fn: SetupPrivateServiceConnectEndpoints},
		{Kind: "ActiveActivePrivateServiceConnectEndpoints", Fn: SetupActiveActivePrivateServiceConnectEndpoints},
		{Kind: "SubscriptionPeerings", Fn: SetupSubscriptionPeerings},
		{Kind: "TransitGateway", Fn: SetupTransitGateway},
		{Kind: "ActiveActiveTransitGateway", Fn: SetupActiveActiveTransitGateway},
		{Kind: "TransitGatewayInvitations", Fn: SetupTransitGatewayInvitations},
		{Kind: "ActiveActiveTransitGatewayInvitations", Fn: SetupActiveActiveTransitGatewayInvitations},
		{Kind: "ActiveActiveSubscriptionRegions", Fn: SetupActiveActiveSubscriptionRegions},
	}
}
