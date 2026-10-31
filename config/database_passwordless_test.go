package config

import (
	"reflect"
	"testing"

	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	subscriptionv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1alpha1"
	subscriptionv1beta1 "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
)

const (
	tfEnablePasswordless = "enable_passwordless"
	tfPassword           = "password"
)

func TestProDatabaseSchemaKeepsEnablePasswordless(t *testing.T) {
	t.Parallel()
	provider := GetProvider()
	resource, ok := provider.Resources[tfSubscriptionDatabase]
	if !ok || resource.TerraformResource == nil {
		t.Fatal("rediscloud_subscription_database is missing from the provider schema")
	}
	attribute, ok := resource.TerraformResource.Schema[tfEnablePasswordless]
	if !ok || attribute == nil {
		t.Fatal("pinned schema dropped enable_passwordless")
	}
	if attribute.Type != schema.TypeBool || !attribute.Optional || attribute.Required {
		t.Fatalf("enable_passwordless schema: type %s optional %t required %t", attribute.Type, attribute.Optional, attribute.Required)
	}
	if !passwordlessDefaultIsSafe(attribute.Default) {
		t.Fatalf("enable_passwordless default: %#v", attribute.Default)
	}
}

func TestProDatabaseTypesKeepEnablePasswordless(t *testing.T) {
	t.Parallel()
	types := []reflect.Type{
		reflect.TypeOf(subscriptionv1beta1.DatabaseParameters{}),
		reflect.TypeOf(subscriptionv1beta1.DatabaseInitParameters{}),
		reflect.TypeOf(subscriptionv1alpha1.DatabaseParameters{}),
		reflect.TypeOf(subscriptionv1alpha1.DatabaseInitParameters{}),
	}
	for _, typ := range types {
		field, ok := typ.FieldByName("EnablePasswordless")
		if !ok {
			t.Fatalf("%s dropped EnablePasswordless", typ.Name())
		}
		if field.Type != reflect.TypeOf((*bool)(nil)) {
			t.Fatalf("%s EnablePasswordless is %s", typ.Name(), field.Type)
		}
		if field.Tag.Get("json") != "enablePasswordless,omitempty" {
			t.Fatalf("%s json tag %q", typ.Name(), field.Tag.Get("json"))
		}
		if field.Tag.Get("tf") != "enable_passwordless,omitempty" {
			t.Fatalf("%s tf tag %q", typ.Name(), field.Tag.Get("tf"))
		}
	}
}

func TestOmittedEnablePasswordlessSendsNoPassword(t *testing.T) {
	t.Parallel()
	database := &subscriptionv1beta1.Database{}
	database.Spec.ForProvider.PasswordSecretRef = &xpv2.SecretKeySelector{
		SecretReference: xpv2.SecretReference{Name: "example", Namespace: "default"},
		Key:             tfPassword,
	}
	params := databaseParameters(t, database)
	if _, ok := params[tfEnablePasswordless]; ok {
		t.Fatal("omitted enablePasswordless was sent")
	}
	if _, ok := params[tfPassword]; ok {
		t.Fatal("omitted enablePasswordless wrote a password")
	}
	merged, err := database.GetMergedParameters(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := merged[tfEnablePasswordless]; ok {
		t.Fatal("merged init parameters sent enable_passwordless")
	}
	if _, ok := merged[tfPassword]; ok {
		t.Fatal("merged init parameters wrote a password")
	}
}

func TestEnablePasswordlessTrueSendsTheAttributeOnly(t *testing.T) {
	t.Parallel()
	enabled := true
	database := &subscriptionv1beta1.Database{}
	database.Spec.ForProvider.EnablePasswordless = &enabled
	params := databaseParameters(t, database)
	if params[tfEnablePasswordless] != true {
		t.Fatalf("enable_passwordless: %#v", params[tfEnablePasswordless])
	}
	if _, ok := params[tfPassword]; ok {
		t.Fatal("true wrote a password parameter; the Terraform provider writes the empty password")
	}
}

func TestExplicitPasswordlessFalseSendsNoPassword(t *testing.T) {
	t.Parallel()
	disabled := false
	database := &subscriptionv1beta1.Database{}
	database.Spec.ForProvider.EnablePasswordless = &disabled
	params := databaseParameters(t, database)
	if params[tfEnablePasswordless] != false {
		t.Fatalf("enable_passwordless: %#v", params[tfEnablePasswordless])
	}
	if _, ok := params[tfPassword]; ok {
		t.Fatal("explicit false wrote a password")
	}
}

func TestLateInitKeepsOmittedPasswordless(t *testing.T) {
	t.Parallel()
	database := &subscriptionv1beta1.Database{}
	changed, err := database.LateInitialize([]byte(`{"enable_passwordless":false,"password":"existing"}`))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("observed false changed the spec")
	}
	if database.Spec.ForProvider.EnablePasswordless != nil {
		t.Fatal("observed false was copied into spec")
	}
	params := databaseParameters(t, database)
	if _, ok := params[tfEnablePasswordless]; ok {
		t.Fatal("late-init of false sent enable_passwordless")
	}
	if _, ok := params[tfPassword]; ok {
		t.Fatal("late-init copied a password into parameters")
	}
}

func TestLateInitAdoptsObservedPasswordless(t *testing.T) {
	t.Parallel()
	database := &subscriptionv1beta1.Database{}
	changed, err := database.LateInitialize([]byte(`{"enable_passwordless":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("observed true was skipped")
	}
	if database.Spec.ForProvider.EnablePasswordless == nil || !*database.Spec.ForProvider.EnablePasswordless {
		t.Fatal("observed true was not adopted")
	}
	params := databaseParameters(t, database)
	if params[tfEnablePasswordless] != true {
		t.Fatalf("enable_passwordless: %#v", params[tfEnablePasswordless])
	}
	if _, ok := params[tfPassword]; ok {
		t.Fatal("adopting enable_passwordless wrote a password parameter")
	}
}

func passwordlessDefaultIsSafe(value any) bool {
	if value == nil {
		return true
	}
	typed, ok := value.(bool)
	return ok && !typed
}

func databaseParameters(t *testing.T, database *subscriptionv1beta1.Database) map[string]any {
	t.Helper()
	params, err := database.GetParameters()
	if err != nil {
		t.Fatal(err)
	}
	return params
}
