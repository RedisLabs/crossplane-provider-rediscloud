/*
Copyright 2022 Upbound Inc.
*/

// Package config contains the provider configuration logic.
package config

import "github.com/crossplane/upjet/pkg/config"

// ExternalNameConfigs contains all external name configurations for this
// provider.
var ExternalNameConfigs = map[string]config.ExternalName{
	// ACL Resources
	"rediscloud_acl_role": config.IdentifierFromProvider,
	"rediscloud_acl_rule": config.IdentifierFromProvider,
	"rediscloud_acl_user": config.IdentifierFromProvider,

	// Active-Active Resources
	"rediscloud_active_active_private_link":                              config.IdentifierFromProvider,
	"rediscloud_active_active_subscription":                              config.IdentifierFromProvider,
	"rediscloud_active_active_subscription_database":                     config.IdentifierFromProvider,
	"rediscloud_active_active_subscription_peering":                      config.IdentifierFromProvider,
	"rediscloud_active_active_subscription_regions":                      config.IdentifierFromProvider,
	"rediscloud_active_active_transit_gateway_attachment":                config.IdentifierFromProvider,
	"rediscloud_active_active_transit_gateway_invitation_acceptor":       config.IdentifierFromProvider,
	"rediscloud_active_active_transit_gateway_route":                     config.IdentifierFromProvider,
	"rediscloud_active_active_private_service_connect":                   config.IdentifierFromProvider,
	"rediscloud_active_active_private_service_connect_endpoint":          config.IdentifierFromProvider,
	"rediscloud_active_active_private_service_connect_endpoint_accepter": config.IdentifierFromProvider,

	// Cloud Resources
	"rediscloud_cloud_account": config.IdentifierFromProvider,

	// Essentials Resources
	"rediscloud_essentials_database":     config.IdentifierFromProvider,
	"rediscloud_essentials_subscription": config.IdentifierFromProvider,

	// PrivateLink Resources
	"rediscloud_private_link": config.IdentifierFromProvider,

	// Private Service Connect Resources
	"rediscloud_private_service_connect":                   config.IdentifierFromProvider,
	"rediscloud_private_service_connect_endpoint":          config.IdentifierFromProvider,
	"rediscloud_private_service_connect_endpoint_accepter": config.IdentifierFromProvider,

	// Pro Subscription Resources
	"rediscloud_subscription":          config.IdentifierFromProvider,
	"rediscloud_subscription_database": config.IdentifierFromProvider,
	"rediscloud_subscription_peering":  config.IdentifierFromProvider,

	// Transit Gateway Resources
	"rediscloud_transit_gateway_attachment":          config.IdentifierFromProvider,
	"rediscloud_transit_gateway_invitation_acceptor": config.IdentifierFromProvider,
	"rediscloud_transit_gateway_route":               config.IdentifierFromProvider,
}

// ExternalNameConfigurations applies the external name configs listed in
// ExternalNameConfigs. Managed resources stay at v1alpha1.
func ExternalNameConfigurations() config.ResourceOption {
	return func(r *config.Resource) {
		if e, ok := ExternalNameConfigs[r.Name]; ok {
			r.ExternalName = e
		}
	}
}

// ExternalNameConfigured returns the list of all resources whose external name
// is configured manually.
func ExternalNameConfigured() []string {
	l := make([]string, len(ExternalNameConfigs))
	i := 0
	for name := range ExternalNameConfigs {
		// $ is added to match the exact string since the format is regex.
		l[i] = name + "$"
		i++
	}
	return l
}
