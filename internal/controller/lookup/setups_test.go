// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import "testing"

func TestSetupsRegistersEveryLookup(t *testing.T) {
	want := []string{
		"PaymentMethod",
		"EssentialsPlan",
		"Regions",
		"ACLRole",
		"ACLRule",
		"ACLUser",
		"CloudAccount",
		"DataPersistence",
		"DatabaseModules",
		"EssentialsSubscription",
		"PrivateLink",
		"ActiveActivePrivateLink",
		"PrivateLinkEndpointScript",
		"ActiveActivePrivateLinkEndpointScript",
		"PrivateServiceConnect",
		"ActiveActivePrivateServiceConnect",
		"PrivateServiceConnectEndpoints",
		"ActiveActivePrivateServiceConnectEndpoints",
		"SubscriptionPeerings",
		"TransitGateway",
		"ActiveActiveTransitGateway",
		"TransitGatewayInvitations",
		"ActiveActiveTransitGatewayInvitations",
		"ActiveActiveSubscriptionRegions",
	}
	got := Setups()
	if len(got) != len(want) {
		t.Fatalf("setups = %d, want %d", len(got), len(want))
	}
	for i, kind := range want {
		if got[i].Kind != kind || got[i].Fn == nil {
			t.Fatalf("setup %d = %s", i, got[i].Kind)
		}
	}
}
