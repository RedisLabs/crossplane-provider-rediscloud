package config

import (
	"testing"
)

func TestGetProvider(t *testing.T) {
	provider := GetProvider()

	if provider == nil {
		t.Fatal("GetProvider() returned nil")
	}

	// Test that required fields are set
	if provider.ShortName != "rediscloud" {
		t.Errorf("Expected ShortName to be 'rediscloud', got %s", provider.ShortName)
	}

	// Test that resources are configured
	if len(provider.Resources) == 0 {
		t.Error("Expected provider to have configured resources")
	}

	// Test that default resource options are set
	if provider.DefaultResourceOptions == nil {
		t.Error("Expected DefaultResourceOptions to be set")
	}

	// Test provider module path
	expectedModulePath := "github.com/RedisLabs/provider-rediscloud"
	if provider.ModulePath != expectedModulePath {
		t.Errorf("Expected ModulePath to be '%s', got '%s'", expectedModulePath, provider.ModulePath)
	}

	// Test that IncludeList is configured
	if len(provider.IncludeList) == 0 {
		t.Error("Expected IncludeList to have entries")
	}
}

func TestProviderResources(t *testing.T) {
	provider := GetProvider()

	// Test that external name configurations exist for key resources
	expectedResources := []string{
		tfSubscription,
		tfSubscriptionDatabase,
		"rediscloud_subscription_peering",
	}

	for _, resourceName := range expectedResources {
		if _, ok := provider.Resources[resourceName]; !ok {
			t.Errorf("Expected resource %s to be configured", resourceName)
		}
	}
}
