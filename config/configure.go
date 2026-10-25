package config

import (
	"fmt"

	ujconfig "github.com/crossplane/upjet/pkg/config"
)

// subscriptionReferenceParents are resources whose subscription_id is a
// string. Database subscription IDs are numbers, and Crossplane reference
// resolution only assigns strings, so those resources are intentionally
// omitted.
var subscriptionReferenceParents = map[string]string{
	"rediscloud_active_active_private_link":                              "rediscloud_active_active_subscription",
	"rediscloud_active_active_private_service_connect":                   "rediscloud_active_active_subscription",
	"rediscloud_active_active_private_service_connect_endpoint":          "rediscloud_active_active_subscription",
	"rediscloud_active_active_private_service_connect_endpoint_accepter": "rediscloud_active_active_subscription",
	"rediscloud_active_active_subscription_peering":                      "rediscloud_active_active_subscription",
	"rediscloud_active_active_subscription_regions":                      "rediscloud_active_active_subscription",
	"rediscloud_active_active_transit_gateway_attachment":                "rediscloud_active_active_subscription",
	"rediscloud_active_active_transit_gateway_invitation_acceptor":       "rediscloud_active_active_subscription",
	"rediscloud_active_active_transit_gateway_route":                     "rediscloud_active_active_subscription",
	"rediscloud_private_link":                                            "rediscloud_subscription",
	"rediscloud_private_service_connect":                                 "rediscloud_subscription",
	"rediscloud_private_service_connect_endpoint":                        "rediscloud_subscription",
	"rediscloud_private_service_connect_endpoint_accepter":               "rediscloud_subscription",
	"rediscloud_subscription_peering":                                    "rediscloud_subscription",
	"rediscloud_transit_gateway_attachment":                              "rediscloud_subscription",
	"rediscloud_transit_gateway_invitation_acceptor":                     "rediscloud_subscription",
	"rediscloud_transit_gateway_route":                                   "rediscloud_subscription",
}

var databaseResources = []string{
	"rediscloud_active_active_subscription_database",
	"rediscloud_essentials_database",
	"rediscloud_subscription_database",
}

func configure(p *ujconfig.Provider) {
	for name, parent := range subscriptionReferenceParents {
		tfName := name
		parentName := parent
		p.AddResourceConfigurator(tfName, func(r *ujconfig.Resource) {
			r.References["subscription_id"] = ujconfig.Reference{
				TerraformName: parentName,
			}
		})
	}
	for _, name := range databaseResources {
		tfName := name
		p.AddResourceConfigurator(tfName, func(r *ujconfig.Resource) {
			r.Sensitive.AdditionalConnectionDetailsFn = databaseConnectionDetails
		})
	}
}

// databaseConnectionDetails publishes the endpoints and port that Terraform
// computes. A Pro or Essentials endpoint is one string and keeps its own key.
// An Active-Active endpoint is a map keyed by region. Each region is stored
// as "<key>.<region>", for example public_endpoint.us-east-1. Port stays one
// value. The password stays on its secret reference and is not copied here,
// because that key is already reserved by the sensitive-field mapping.
func databaseConnectionDetails(attr map[string]any) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, key := range []string{"public_endpoint", "private_endpoint", "port"} {
		if err := putConnectionValue(out, key, attr[key]); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	return out, nil
}

func putConnectionValue(out map[string][]byte, key string, value any) error {
	regions, ok := value.(map[string]any)
	if ok {
		return putRegionEndpoints(out, key, regions)
	}
	text, ok, err := connectionString(value)
	if err != nil {
		return err
	}
	if ok {
		putSecret(out, key, text)
	}
	return nil
}

func putRegionEndpoints(out map[string][]byte, key string, regions map[string]any) error {
	for region, raw := range regions {
		if region == "" {
			return fmt.Errorf("region name is empty")
		}
		text, ok, err := connectionString(raw)
		if err != nil {
			return fmt.Errorf("region %s: %w", region, err)
		}
		if ok {
			putSecret(out, key+"."+region, text)
		}
	}
	return nil
}

func putSecret(out map[string][]byte, key, value string) {
	if value == "" {
		return
	}
	out[key] = []byte(value)
}

func connectionString(value any) (string, bool, error) {
	switch typed := value.(type) {
	case nil:
		return "", false, nil
	case string:
		return typed, true, nil
	case float64:
		return fmt.Sprintf("%g", typed), true, nil
	default:
		return "", false, fmt.Errorf("unsupported type %T", value)
	}
}
