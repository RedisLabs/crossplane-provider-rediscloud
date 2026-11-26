package qa_envtest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestACLUserPasswordAdmission(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("hosted Actions only")
	}
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("verified hosted API server assets required")
	}
	crd, err := os.ReadFile(filepath.Join("..", "..", "package", "crds", "acl.redis.io_users.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "users.yaml"), crd, 0o600); err != nil {
		t.Fatal(err)
	}
	environment := &envtest.Environment{CRDDirectoryPaths: []string{dir}, ErrorIfCRDPathMissing: true}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop API server: %v", err)
		}
	})
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1alpha1", "v1beta1"} {
		for _, test := range []struct {
			name     string
			policies []any
			secret   bool
			valid    bool
		}{
			{"observe", []any{"Observe"}, false, true},
			{"create_no_secret", []any{"Create"}, false, false},
			{"update_no_secret", []any{"Update"}, false, false},
			{"default_no_secret", nil, false, false},
			{"create_partial_secret", []any{"Create"}, true, false},
			{"create_with_secret", []any{"Create"}, true, true},
			{"update_with_secret", []any{"Update"}, true, true},
		} {
			t.Run(version+"/"+test.name, func(t *testing.T) {
				parameters := map[string]any{"name": "fixture", "role": "fixture-role"}
				if test.secret {
					parameters["passwordSecretRef"] = map[string]any{"name": "fixture-secret", "namespace": "default", "key": "password"}
					if test.name == "create_partial_secret" {
						delete(parameters["passwordSecretRef"].(map[string]any), "key")
					}
				}
				spec := map[string]any{"forProvider": parameters}
				if test.policies != nil {
					spec["managementPolicies"] = test.policies
				}
				obj := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "acl.redis.io/" + version, "kind": "User",
					"metadata": map[string]any{"name": version + "-" + strings.ReplaceAll(test.name, "_", "-")}, "spec": spec,
				}}
				_, err := client.Resource(schema.GroupVersionResource{Group: "acl.redis.io", Version: version, Resource: "users"}).Create(context.Background(), obj, metav1.CreateOptions{})
				if test.valid && err != nil {
					t.Fatalf("expected admission: %v", err)
				}
				if test.name == "observe" && err == nil {
					other := "v1beta1"
					if version == other {
						other = "v1alpha1"
					}
					got, getErr := client.Resource(schema.GroupVersionResource{Group: "acl.redis.io", Version: other, Resource: "users"}).Get(context.Background(), version+"-"+test.name, metav1.GetOptions{})
					if getErr != nil {
						t.Fatalf("cross-version read: %v", getErr)
					}
					_, found, getErr := unstructured.NestedFieldNoCopy(got.Object, "spec", "forProvider", "passwordSecretRef")
					if getErr != nil || found {
						t.Fatalf("cross-version observation unexpectedly included password selector: %v", getErr)
					}
					policies, found, getErr := unstructured.NestedStringSlice(got.Object, "spec", "managementPolicies")
					if getErr != nil || !found || len(policies) != 1 || policies[0] != "Observe" {
						t.Fatalf("cross-version management policy changed: %v", getErr)
					}
				}
				if !test.valid && (err == nil || !apierrors.IsInvalid(err) || !strings.Contains(err.Error(), "passwordSecretRef")) {
					t.Fatalf("expected password-specific validation rejection, got: %v", err)
				}
			})
		}
	}
}
