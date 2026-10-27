package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionReferences(t *testing.T) {
	t.Parallel()
	provider := GetProvider()
	for name, parent := range subscriptionReferenceParents {
		resource, ok := provider.Resources[name]
		if !ok {
			t.Fatalf("resource %s was not configured", name)
		}
		ref, ok := resource.References["subscription_id"]
		if !ok {
			t.Fatalf("resource %s has no subscription_id reference", name)
		}
		if ref.TerraformName != parent {
			t.Fatalf("resource %s references %s, want %s", name, ref.TerraformName, parent)
		}
	}
	for _, name := range []string{
		"rediscloud_subscription_database",
		"rediscloud_essentials_database",
		"rediscloud_active_active_subscription_database",
	} {
		if _, ok := provider.Resources[name].References["subscription_id"]; ok {
			t.Fatalf("numeric subscription_id on %s must not use a string reference", name)
		}
		if provider.Resources[name].Sensitive.AdditionalConnectionDetailsFn == nil {
			t.Fatalf("database %s does not publish connection details", name)
		}
	}
}

func TestDatabaseConnectionDetails(t *testing.T) {
	t.Parallel()
	t.Run("port already in terraform state", func(t *testing.T) {
		got, err := databaseConnectionDetails(map[string]any{
			"public_endpoint":  "redis.example:12345",
			"private_endpoint": "",
			"port":             float64(12345),
			"password":         "do-not-copy",
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got["public_endpoint"]) != "redis.example:12345" {
			t.Fatalf("public endpoint: %s", got["public_endpoint"])
		}
		if string(got["port"]) != "12345" {
			t.Fatalf("port: %s", got["port"])
		}
		if _, ok := got["password"]; ok {
			t.Fatal("password must stay out of the additional connection details")
		}
		if _, ok := got["private_endpoint"]; ok {
			t.Fatal("empty private endpoint should be omitted")
		}
	})

	// The 27 October 2026 QA database had both endpoints set and no port.
	t.Run("live observation without port", func(t *testing.T) {
		got, err := databaseConnectionDetails(map[string]any{
			"public_endpoint":  "redis.example:12345",
			"private_endpoint": "redis.internal:12345",
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got["public_endpoint"]) != "redis.example:12345" {
			t.Fatalf("public endpoint: %s", got["public_endpoint"])
		}
		if string(got["private_endpoint"]) != "redis.internal:12345" {
			t.Fatalf("private endpoint: %s", got["private_endpoint"])
		}
		if _, ok := got["port"]; ok {
			t.Fatal("port must stay absent when Terraform state has no port")
		}
		if _, ok := got["password"]; ok {
			t.Fatal("password must stay out of the additional connection details")
		}
		if len(got) != 2 {
			t.Fatalf("secret keys: %v", got)
		}
	})
}

func TestActiveActiveDatabaseConnectionDetails(t *testing.T) {
	t.Parallel()
	got := connectionDetailsFromJSON(t, `{
		"public_endpoint": {"us-east-1": "redis-east.example:12000", "eu-west-1": ""},
		"private_endpoint": {"us-east-1": "redis-east.internal:12000", "eu-west-1": null},
		"port": 12000,
		"password": "do-not-copy"
	}`)
	want := map[string]string{
		"public_endpoint.us-east-1":  "redis-east.example:12000",
		"private_endpoint.us-east-1": "redis-east.internal:12000",
		"port":                       "12000",
	}
	for key, value := range want {
		if string(got[key]) != value {
			t.Fatalf("%s = %s", key, got[key])
		}
	}
	for _, key := range []string{"public_endpoint", "public_endpoint.eu-west-1", "private_endpoint.eu-west-1", "password"} {
		if _, ok := got[key]; ok {
			t.Fatalf("unexpected key %s", key)
		}
	}

	_, err := databaseConnectionDetails(map[string]any{
		"public_endpoint": map[string]any{"us-east-1": map[string]any{"host": "nope"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("nested endpoint: %v", err)
	}
}

func connectionDetailsFromJSON(t *testing.T, raw string) map[string][]byte {
	t.Helper()
	var attr map[string]any
	if err := json.Unmarshal([]byte(raw), &attr); err != nil {
		t.Fatal(err)
	}
	got, err := databaseConnectionDetails(attr)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
