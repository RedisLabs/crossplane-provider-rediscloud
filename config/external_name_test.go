package config

import (
	"testing"

	"github.com/crossplane/upjet/v2/pkg/config"
)

func TestExternalNameConfigured(t *testing.T) {
	result := ExternalNameConfigured()

	if len(result) == 0 {
		t.Error("Expected ExternalNameConfigured to return non-empty list")
	}

	// Check that all entries end with $
	for _, name := range result {
		if name[len(name)-1] != '$' {
			t.Errorf("Expected resource name to end with $, got %s", name)
		}
	}

	// Check that some known resources are included
	expectedResources := []string{
		"rediscloud_subscription$",
		"rediscloud_subscription_database$",
		"rediscloud_cloud_account$",
	}

	resultMap := make(map[string]bool)
	for _, r := range result {
		resultMap[r] = true
	}

	for _, expected := range expectedResources {
		if !resultMap[expected] {
			t.Errorf("Expected resource %s to be in the list", expected)
		}
	}
}

func TestExternalNameConfigurations(_ *testing.T) {
	// Test that the function applies configurations correctly
	// We'll test by checking if it runs without panic
	r := &config.Resource{
		Name: tfSubscription,
	}

	// This should not panic and should apply the configuration
	ExternalNameConfigurations()(r)

	// Test with various resource names
	testResources := []string{
		tfSubscription,
		tfSubscriptionDatabase,
		"unknown_resource",
	}

	for _, name := range testResources {
		r := &config.Resource{
			Name: name,
		}
		// Should not panic
		ExternalNameConfigurations()(r)
	}
}

func TestExternalNameConfigs(t *testing.T) {
	// Test that ExternalNameConfigs contains expected resources
	expectedResources := []string{
		tfSubscription,
		tfSubscriptionDatabase,
		"rediscloud_subscription_peering",
		"rediscloud_cloud_account",
		"rediscloud_acl_user",
		"rediscloud_acl_rule",
	}

	for _, resourceName := range expectedResources {
		if _, ok := ExternalNameConfigs[resourceName]; !ok {
			t.Errorf("Expected resource %s to be in ExternalNameConfigs", resourceName)
		}
	}

	// Check that all entries exist (we can't compare ExternalName structs directly)
	if len(ExternalNameConfigs) == 0 {
		t.Error("ExternalNameConfigs should not be empty")
	}
}
