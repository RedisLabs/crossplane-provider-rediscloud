package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	commonv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	activev1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/active/v1alpha1"
	essentialsv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/essentials/v1alpha1"
	subscriptionv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1alpha1"
)

func TestControllersUseThirtyMinuteTimeout(t *testing.T) {
	t.Parallel()
	files := controllerFiles(t, moduleRoot(t))
	if len(files) != 27 {
		t.Fatalf("generated controllers: got %d, want 27", len(files))
	}
	if problems := timeoutProblems(files); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

func TestThreeMinuteControllerTimeoutIsReported(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "internal", "controller", "example", "example")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "zz_controller.go")
	body := []byte("managed.WithTimeout(3 * time.Minute)\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	problems := timeoutProblems([]string{path})
	if !strings.Contains(strings.Join(problems, "\n"), "3 * time.Minute") {
		t.Fatalf("problems: %q", problems)
	}
}

func TestActiveActiveTLSInitIsSecretRefList(t *testing.T) {
	t.Parallel()
	types := []reflect.Type{
		reflect.TypeOf(activev1alpha1.ActiveSubscriptionDatabaseInitParameters{}),
		reflect.TypeOf(activev1alpha1.ActiveSubscriptionDatabaseParameters{}),
	}
	for _, typ := range types {
		if problem := secretRefListProblem(typ, "ClientTLSCertificatesSecretRef"); problem != "" {
			t.Fatalf("%s: %s", typ.Name(), problem)
		}
	}
}

func TestSecretRefListProblemRejectsAPlainString(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(struct {
		ClientTLSCertificatesSecretRef *string
	}{})
	if secretRefListProblem(typ, "ClientTLSCertificatesSecretRef") == "" {
		t.Fatal("a plain string was accepted as a secret-reference list")
	}
}

func TestDatabasesHaveNoSubscriptionIDRef(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeOf(subscriptionv1alpha1.DatabaseParameters{}),
		reflect.TypeOf(subscriptionv1alpha1.DatabaseInitParameters{}),
		reflect.TypeOf(essentialsv1alpha1.DatabaseParameters{}),
		reflect.TypeOf(essentialsv1alpha1.DatabaseInitParameters{}),
		reflect.TypeOf(activev1alpha1.ActiveSubscriptionDatabaseParameters{}),
		reflect.TypeOf(activev1alpha1.ActiveSubscriptionDatabaseInitParameters{}),
	} {
		if _, ok := typ.FieldByName("SubscriptionIDRef"); ok {
			t.Fatalf("%s has SubscriptionIDRef", typ.Name())
		}
	}
}

func TestStringChildrenKeepSubscriptionIDRef(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeOf(subscriptionv1alpha1.PeeringParameters{}),
		reflect.TypeOf(subscriptionv1alpha1.PeeringInitParameters{}),
	} {
		field, ok := typ.FieldByName("SubscriptionIDRef")
		if !ok {
			t.Fatalf("%s lost SubscriptionIDRef", typ.Name())
		}
		if field.Type != reflect.TypeOf((*commonv1.Reference)(nil)) {
			t.Fatalf("%s SubscriptionIDRef is %s", typ.Name(), field.Type)
		}
	}
}

func controllerFiles(t *testing.T, root string) []string {
	t.Helper()
	pattern := filepath.Join(root, "internal", "controller", "*", "*", "zz_controller.go")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func timeoutProblems(files []string) []string {
	var problems []string
	for _, match := range files {
		raw, err := os.ReadFile(match)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		text := string(raw)
		if strings.Contains(text, "3 * time.Minute") || strings.Contains(text, "3*time.Minute") {
			problems = append(problems, match+" still uses 3 * time.Minute")
		}
		if !strings.Contains(text, "30 * time.Minute") {
			problems = append(problems, match+" is missing 30 * time.Minute")
		}
	}
	return problems
}

func secretRefListProblem(typ reflect.Type, name string) string {
	field, ok := typ.FieldByName(name)
	if !ok {
		return name + " is missing"
	}
	if !isSecretRefList(field.Type) {
		return name + " is " + field.Type.String()
	}
	return ""
}

func isSecretRefList(typ reflect.Type) bool {
	if typ.Kind() != reflect.Pointer || typ.Elem().Kind() != reflect.Slice {
		return false
	}
	return typ.Elem().Elem() == reflect.TypeOf(commonv1.SecretKeySelector{})
}
