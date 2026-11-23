// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"errors"
	"testing"
	"time"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/roles"
	"github.com/RedisLabs/rediscloud-go-api/service/cloud_accounts"
	"github.com/RedisLabs/rediscloud-go-api/service/fixed/subscriptions"
	"github.com/RedisLabs/rediscloud-go-api/service/psc"
	prosubs "github.com/RedisLabs/rediscloud-go-api/service/subscriptions"
	"github.com/RedisLabs/rediscloud-go-api/service/transit_gateway/attachments"
)

func TestMatchCloudAccountExcludeInternalOnlyWhenTrue(t *testing.T) {
	internal := cloudAccount(1, "internal", "AWS")
	named := cloudAccount(2, "example-account", cloudProviderGCP)
	accounts := []*cloud_accounts.CloudAccount{internal, named}

	got, err := MatchCloudAccount(accounts, CloudAccountQuery{Name: "internal"})
	if err != nil || redis.IntValue(got.ID) != 1 {
		t.Fatalf("unset exclude: %v %#v", err, got)
	}

	got, err = MatchCloudAccount(accounts, CloudAccountQuery{ExcludeInternal: false, Name: "internal"})
	if err != nil || redis.IntValue(got.ID) != 1 {
		t.Fatalf("false exclude: %v %#v", err, got)
	}

	got, err = MatchCloudAccount(accounts, CloudAccountQuery{ExcludeInternal: true, ProviderType: cloudProviderGCP})
	if err != nil || redis.IntValue(got.ID) != 2 {
		t.Fatalf("exclude internal: %v %#v", err, got)
	}

	_, err = MatchCloudAccount(accounts, CloudAccountQuery{})
	if !errors.Is(err, ErrMany) {
		t.Fatalf("many: %v", err)
	}
}

func TestMatchEssentialsSubscriptionTreatsZeroIDAsUnset(t *testing.T) {
	one := fixedSubscription(1001, "example-essentials")
	two := fixedSubscription(1002, "example-other")
	items := []*subscriptions.FixedSubscriptionResponse{one, two}

	got, err := MatchEssentialsSubscription(items, EssentialsSubscriptionQuery{ID: "0", Name: "example-essentials"})
	if err != nil || redis.IntValue(got.ID) != 1001 {
		t.Fatalf("zero id: %v %#v", err, got)
	}

	_, err = MatchEssentialsSubscription(items, EssentialsSubscriptionQuery{ID: "0"})
	if !errors.Is(err, ErrMany) {
		t.Fatalf("unset id: %v", err)
	}

	observed := ObserveEssentialsSubscription(one)
	if observed.PaymentMethodID != "0" {
		t.Fatalf("payment method = %s", observed.PaymentMethodID)
	}
	if observed.CreationDate != redis.TimeValue(one.CreationDate).String() {
		t.Fatalf("creation date = %s", observed.CreationDate)
	}
}

func TestFilterPeeringsKeepsEmptyListReady(t *testing.T) {
	aws := &prosubs.VPCPeering{ID: redis.Int(8), Status: redis.String("active"), VPCId: redis.String("example-vpc")}
	gcp := &prosubs.VPCPeering{ID: redis.Int(9), Status: redis.String("inactive"), GCPProjectUID: redis.String("example-gcp-project")}
	unknown := &prosubs.VPCPeering{ID: redis.Int(10), Status: redis.String("active")}

	all := FilterPeerings([]*prosubs.VPCPeering{aws, gcp, nil}, "")
	if all.ID != "ALL" || len(all.Peerings) != 2 {
		t.Fatalf("all = %#v", all)
	}
	if all.Peerings[0].ProviderName != "AWS" || all.Peerings[1].ProviderName != cloudProviderGCP {
		t.Fatalf("providers = %#v", all.Peerings)
	}

	active := FilterPeerings([]*prosubs.VPCPeering{aws, gcp, unknown}, "active")
	if active.ID != "active" || len(active.Peerings) != 2 || active.Peerings[1].ProviderName != "Unknown" {
		t.Fatalf("active = %#v", active)
	}

	empty := FilterPeerings(nil, "active")
	if empty.ID != "active" || empty.Peerings != nil {
		t.Fatalf("empty = %#v", empty)
	}
}

func TestMatchTransitGatewayRejectsNilTask(t *testing.T) {
	_, err := MatchTransitGateway(nil, TransitGatewayQuery{})
	if err == nil || err.Error() != "transit gateway API returned nil task" {
		t.Fatalf("nil task: %v", err)
	}

	task := &attachments.GetAttachmentsTask{Response: &attachments.Response{}}
	_, err = MatchTransitGateway(task, TransitGatewayQuery{})
	if err == nil || err.Error() != "transit gateway API returned nil resource" {
		t.Fatalf("nil resource: %v", err)
	}

	attachment := &attachments.TransitGatewayAttachment{
		Id:        redis.Int(4),
		AwsTgwUid: redis.String("example-tgw"),
		Cidrs:     []*attachments.Cidr{{CidrAddress: redis.String("10.0.0.0/16")}},
	}
	task.Response.Resource = &attachments.Resource{TransitGatewayAttachment: []*attachments.TransitGatewayAttachment{attachment}}
	got, err := MatchTransitGateway(task, TransitGatewayQuery{AWSTGWUID: "example-tgw"})
	if err != nil {
		t.Fatal(err)
	}
	observed := ObserveTransitGateway(1001, got)
	if observed.ID != "1001/4" || len(observed.CIDRs) != 1 || observed.CIDRs[0] != "10.0.0.0/16" {
		t.Fatalf("observed = %#v", observed)
	}
}

func TestEndpointScriptSkippedForRejectedAndDeleted(t *testing.T) {
	if EndpointNeedsScript(psc.EndpointStatusRejected) || EndpointNeedsScript(psc.EndpointStatusDeleted) {
		t.Fatal("rejected and deleted endpoints ask for a creation script")
	}
	if !EndpointNeedsScript(psc.EndpointStatusActive) || !IncludeTerraformGCPScript {
		t.Fatal("active endpoints skip the Terraform GCP script")
	}
	if _, err := ObserveServiceAttachments(nil); err == nil {
		t.Fatal("missing script did not fail")
	}
}

func TestMatchActiveActiveSubscriptionIgnoresPro(t *testing.T) {
	pro := &prosubs.Subscription{ID: redis.Int(1002), Name: redis.String("example-aa"), DeploymentType: redis.String(prosubs.SubscriptionDeploymentTypeSingleRegion)}
	aa := &prosubs.Subscription{ID: redis.Int(1001), Name: redis.String("example-aa"), DeploymentType: redis.String(prosubs.SubscriptionDeploymentTypeActiveActive)}
	got, err := MatchActiveActiveSubscription([]*prosubs.Subscription{pro, aa}, "example-aa")
	if err != nil || redis.IntValue(got.ID) != 1001 {
		t.Fatalf("match: %v %#v", err, got)
	}

	observed := ObserveActiveActiveRegions(1001, nil)
	if observed.ID != "1001-active-active-regions" || len(observed.Regions) != 0 {
		t.Fatalf("empty regions = %#v", observed)
	}
}

func TestObserveACLRoleCopiesRuleDatabases(t *testing.T) {
	region := "us-east-1"
	role := &roles.GetRoleResponse{
		ID:   redis.Int(3),
		Name: redis.String("example-role"),
		RedisRules: []*roles.GetRuleInRoleResponse{{
			RuleName: redis.String("example-rule"),
			Databases: []*roles.GetDatabaseInRuleInRoleResponse{{
				SubscriptionId: redis.Int(1001),
				DatabaseId:     redis.Int(5),
				Regions:        []*string{&region},
			}},
		}},
	}
	got := ObserveACLRole(role)
	if got.ID != "3" || len(got.Rules) != 1 || got.Rules[0].Name != "example-rule" {
		t.Fatalf("role = %#v", got)
	}
	if got.Rules[0].Databases[0].Subscription != "1001" || got.Rules[0].Databases[0].Regions[0] != "us-east-1" {
		t.Fatalf("database = %#v", got.Rules[0].Databases)
	}
}

func TestParseIDRejectsBlankAndNonNumeric(t *testing.T) {
	if _, err := ParseID("subscription id", ""); err == nil {
		t.Fatal("blank id was accepted")
	}
	if _, err := ParseID("subscription id", "example"); err == nil {
		t.Fatal("non-numeric id was accepted")
	}
	id, err := ParseID("subscription id", "1001")
	if err != nil || id != 1001 {
		t.Fatalf("id = %d %v", id, err)
	}
}

func cloudAccount(id int, name, provider string) *cloud_accounts.CloudAccount {
	return &cloud_accounts.CloudAccount{ID: redis.Int(id), Name: redis.String(name), Provider: redis.String(provider)}
}

func fixedSubscription(id int, name string) *subscriptions.FixedSubscriptionResponse {
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	return &subscriptions.FixedSubscriptionResponse{
		ID:           redis.Int(id),
		Name:         redis.String(name),
		Status:       redis.String("active"),
		PlanId:       redis.Int(7),
		CreationDate: &created,
	}
}
