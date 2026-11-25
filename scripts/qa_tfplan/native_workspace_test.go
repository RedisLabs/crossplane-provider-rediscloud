package qatfplan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	api "github.com/RedisLabs/provider-rediscloud/apis/essentials/v1beta1"
	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// This tests the production WorkspaceStore's configuration regeneration only;
// it does not invoke a provider operation or authenticate a Cloud account.
func TestNativeWorkspaceStoreRegenerates(t *testing.T) {
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("pinned Terraform and provider required")
	}
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	t.Setenv("HOME", root)
	t.Setenv("PATH", filepath.Dir(tf)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TF_CLI_ARGS", "")
	t.Setenv("TF_CLI_ARGS_init", "")
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "http://127.0.0.1:1")
	}
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	configPath := filepath.Join(root, "offline.tfrc")
	config := fmt.Sprintf("provider_installation {\n filesystem_mirror { path = %q }\n}\n", plugins)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TF_CLI_CONFIG_FILE", configPath)
	item := &api.Database{ObjectMeta: metav1.ObjectMeta{Name: fixtureName, UID: types.UID("native-store-fixture")}}
	name, eviction, persistence, replication, sub := fixtureName, desiredEviction, fixtureNoPersistence, false, float64(1)
	item.Spec.ForProvider.Name = &name
	item.Spec.ForProvider.DataEviction = &eviction
	item.Spec.ForProvider.DataPersistence = &persistence
	item.Spec.ForProvider.Replication = &replication
	item.Spec.ForProvider.SubscriptionID = &sub
	store := terraform.NewWorkspaceStore(logging.NewNopLogger())
	setup := terraform.Setup{Version: "1.5.7", Requirement: terraform.ProviderRequirement{Source: fixtureProviderSource, Version: fixtureProviderVersion}, Configuration: terraform.ProviderConfiguration{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: "http://127.0.0.1:1/v1"}}
	cfg := providerconfig.GetProvider().Resources["rediscloud_essentials_database"]
	if _, err := store.Workspace(context.Background(), nil, item, setup, cfg); err != nil {
		t.Fatal(err)
	}
	checkNativeEviction(t, filepath.Join(root, string(item.UID), "main.tf.json"), desiredEviction)
	changed := "volatile-lru"
	item.Spec.ForProvider.DataEviction = &changed
	if _, err := store.Workspace(context.Background(), nil, item, setup, cfg); err != nil {
		t.Fatal(err)
	}
	checkNativeEviction(t, filepath.Join(root, string(item.UID), "main.tf.json"), changed)
}

func checkNativeEviction(t *testing.T, path, expected string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Resource map[string]map[string]map[string]any `json:"resource"`
	}
	if err = json.Unmarshal(b, &document); err != nil {
		t.Fatal(err)
	}
	resource := document.Resource["rediscloud_essentials_database"][fixtureName]
	if resource["data_eviction"] != expected || resource["subscription_id"] != float64(1) {
		t.Fatalf("generated resource differs: %#v", resource)
	}
}
