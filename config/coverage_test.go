package config

import (
	"encoding/json"
	"os"
	"testing"
)

// managedResources is the Terraform v2.20.0 managed resource list. A new
// resource has to be added here and in ExternalNameConfigs together.
var managedResources = []string{
	"rediscloud_acl_role",
	"rediscloud_acl_rule",
	"rediscloud_acl_user",
	"rediscloud_agent_memory",
	"rediscloud_agent_memory_api_key",
	"rediscloud_active_active_private_link",
	"rediscloud_active_active_private_service_connect",
	"rediscloud_active_active_private_service_connect_endpoint",
	"rediscloud_active_active_private_service_connect_endpoint_accepter",
	tfActiveActiveSubscription,
	"rediscloud_active_active_subscription_database",
	"rediscloud_active_active_subscription_peering",
	"rediscloud_active_active_subscription_regions",
	"rediscloud_active_active_transit_gateway_attachment",
	"rediscloud_active_active_transit_gateway_invitation_acceptor",
	"rediscloud_active_active_transit_gateway_route",
	"rediscloud_cloud_account",
	"rediscloud_essentials_database",
	"rediscloud_essentials_subscription",
	"rediscloud_private_link",
	"rediscloud_private_service_connect",
	"rediscloud_private_service_connect_endpoint",
	"rediscloud_private_service_connect_endpoint_accepter",
	tfSubscription,
	tfSubscriptionDatabase,
	"rediscloud_subscription_peering",
	"rediscloud_transit_gateway_attachment",
	"rediscloud_transit_gateway_invitation_acceptor",
	"rediscloud_transit_gateway_route",
}

// newIn219 are the six resources added after Terraform provider v2.1.5.
var newIn219 = []string{
	"rediscloud_active_active_private_link",
	"rediscloud_active_active_transit_gateway_invitation_acceptor",
	"rediscloud_active_active_transit_gateway_route",
	"rediscloud_private_link",
	"rediscloud_transit_gateway_invitation_acceptor",
	"rediscloud_transit_gateway_route",
}

func TestManagedResourceList(t *testing.T) {
	t.Parallel()
	if len(managedResources) != 29 {
		t.Fatalf("managed resource list has %d entries, want 29", len(managedResources))
	}
	seen := map[string]struct{}{}
	for _, name := range managedResources {
		if _, dup := seen[name]; dup {
			t.Fatalf("duplicate managed resource %s", name)
		}
		seen[name] = struct{}{}
		if _, ok := ExternalNameConfigs[name]; !ok {
			t.Errorf("ExternalNameConfigs missing %s", name)
		}
	}
	if len(ExternalNameConfigs) != len(managedResources) {
		t.Fatalf("ExternalNameConfigs has %d entries, list has %d", len(ExternalNameConfigs), len(managedResources))
	}
}

func TestNewResourcesReferenceASubscription(t *testing.T) {
	t.Parallel()
	if len(newIn219) != 6 {
		t.Fatalf("new resource list has %d entries, want 6", len(newIn219))
	}
	for _, name := range newIn219 {
		parent, ok := subscriptionReferenceParents[name]
		if !ok {
			t.Errorf("%s is new in v2.19.2 and has no subscription reference", name)
			continue
		}
		if parent == "" {
			t.Errorf("%s reference parent is empty", name)
		}
	}
}

func TestSchemaResourcesAreConfigured(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ProviderSchemas map[string]struct {
			ResourceSchemas map[string]json.RawMessage `json:"resource_schemas"`
		} `json:"provider_schemas"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.ProviderSchemas) != 1 {
		t.Fatalf("provider schemas: %d", len(doc.ProviderSchemas))
	}
	for _, schema := range doc.ProviderSchemas {
		if len(schema.ResourceSchemas) != len(ExternalNameConfigs) {
			t.Fatalf("schema has %d resources, ExternalNameConfigs has %d", len(schema.ResourceSchemas), len(ExternalNameConfigs))
		}
		for name := range schema.ResourceSchemas {
			if _, ok := ExternalNameConfigs[name]; !ok {
				t.Errorf("schema resource %s is not generated", name)
			}
		}
	}
}
