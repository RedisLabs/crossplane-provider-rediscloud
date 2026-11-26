package qatfplan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Run the real API server only on hosted Actions. The existing fake Cloud and
// pinned provider remain loopback-only; no Kubernetes or Cloud credentials.
func newProEnvtestClient(t *testing.T, scheme *runtime.Scheme, item *pro.Database) client.Client { //nolint:gocyclo // Hosted setup and conflict checks are deliberately kept together.
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != fixtureStringTrue || os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("hosted envtest assets are required")
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	crd, err := os.ReadFile(filepath.Join("..", "..", "package", "crds", "subscription.redis.io_databases.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(dir, "database.yaml"), crd, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	environment := &envtest.Environment{CRDDirectoryPaths: []string{dir}, ErrorIfCRDPathMissing: true}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if stopErr := environment.Stop(); stopErr != nil {
			t.Errorf("stop API server: %v", stopErr)
		}
	})
	if configPath := os.Getenv("QA_RESTART_CONFIG_OUT"); configPath != "" {
		data, encodeErr := json.Marshal(struct {
			Host            string
			TLSClientConfig rest.TLSClientConfig
		}{Host: cfg.Host, TLSClientConfig: cfg.TLSClientConfig})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if writeErr := os.WriteFile(configPath, data, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	kube, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	item.UID = "" // API server owns identity and resourceVersion.
	if err := kube.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if item.UID == "" || item.ResourceVersion == "" {
		t.Fatal("API server did not assign identity and resourceVersion")
	}
	key := types.NamespacedName{Name: item.Name}
	first, stale := &pro.Database{}, &pro.Database{}
	if err := kube.Get(context.Background(), key, first); err != nil {
		t.Fatal(err)
	}
	if err := kube.Get(context.Background(), key, stale); err != nil {
		t.Fatal(err)
	}
	firstAnnotations := first.GetAnnotations()
	if firstAnnotations == nil {
		firstAnnotations = map[string]string{}
	}
	firstAnnotations["fixture.test/cas"] = fixtureFirst
	first.SetAnnotations(firstAnnotations)
	if err := kube.Update(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	stale.SetAnnotations(map[string]string{"fixture.test/cas": "stale"})
	if err := kube.Update(context.Background(), stale); !apierrors.IsConflict(err) {
		t.Fatalf("expected API server stale-version conflict: %v", err)
	}
	if err := kube.Get(context.Background(), key, item); err != nil {
		t.Fatal(err)
	}
	return kube
}
