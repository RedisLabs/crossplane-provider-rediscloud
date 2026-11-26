// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"fmt"
	"strconv"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/redis_rules"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/roles"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/users"
	"github.com/RedisLabs/rediscloud-go-api/service/account"
	"github.com/RedisLabs/rediscloud-go-api/service/cloud_accounts"
	"github.com/RedisLabs/rediscloud-go-api/service/fixed/subscriptions"
	"github.com/RedisLabs/rediscloud-go-api/service/privatelink"
	"github.com/RedisLabs/rediscloud-go-api/service/psc"
	prosubs "github.com/RedisLabs/rediscloud-go-api/service/subscriptions"
	"github.com/RedisLabs/rediscloud-go-api/service/transit_gateway/attachments"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
)

// CloudAccountQuery is the filter for one cloud account.
type CloudAccountQuery struct {
	ExcludeInternal bool
	ProviderType    string
	Name            string
}

// MatchCloudAccount applies the Terraform rediscloud_cloud_account filters
// and returns the single match. ExcludeInternal is applied only when it is
// true, which is when that data source sends the filter.
func MatchCloudAccount(accounts []*cloud_accounts.CloudAccount, query CloudAccountQuery) (*cloud_accounts.CloudAccount, error) {
	matched := make([]*cloud_accounts.CloudAccount, 0, 1)
	for _, item := range accounts {
		if item == nil {
			continue
		}
		if query.ExcludeInternal && redis.IntValue(item.ID) == 1 {
			continue
		}
		if query.ProviderType != "" && redis.StringValue(item.Provider) != query.ProviderType {
			continue
		}
		if query.Name != "" && redis.StringValue(item.Name) != query.Name {
			continue
		}
		matched = append(matched, item)
	}
	return SelectOne(matched)
}

// ObserveCloudAccount copies the fields the cloud account data source writes.
func ObserveCloudAccount(item *cloud_accounts.CloudAccount) lookupv1alpha1.CloudAccountObservation {
	return lookupv1alpha1.CloudAccountObservation{
		ID:           strconv.Itoa(redis.IntValue(item.ID)),
		Name:         redis.StringValue(item.Name),
		AccessKeyID:  redis.StringValue(item.AccessKeyID),
		ProviderType: redis.StringValue(item.Provider),
	}
}

// MatchNamed returns the single item whose name equals want.
func MatchNamed[T any](items []*T, want string, nameOf func(*T) *string) (*T, error) {
	if want == "" {
		return nil, fmt.Errorf("name is required")
	}
	matched := make([]*T, 0, 1)
	for _, item := range items {
		if item == nil {
			return nil, fmt.Errorf("named lookup entry is missing")
		}
		name := nameOf(item)
		if name == nil {
			return nil, fmt.Errorf("named lookup entry name is missing")
		}
		if *name == want {
			matched = append(matched, item)
		}
	}
	return SelectOne(matched)
}

// ObserveACLRole copies the role id, name, and rules.
func ObserveACLRole(role *roles.GetRoleResponse) lookupv1alpha1.ACLRoleObservation {
	rules := make([]lookupv1alpha1.ACLRoleRuleObservation, 0, len(role.RedisRules))
	for _, rule := range role.RedisRules {
		if rule == nil {
			continue
		}
		rules = append(rules, lookupv1alpha1.ACLRoleRuleObservation{
			Name:      redis.StringValue(rule.RuleName),
			Databases: observeRoleDatabases(rule.Databases),
		})
	}
	if len(rules) == 0 {
		rules = nil
	}
	return lookupv1alpha1.ACLRoleObservation{
		ID:    strconv.Itoa(redis.IntValue(role.ID)),
		Name:  redis.StringValue(role.Name),
		Rules: rules,
	}
}

func observeRoleDatabases(items []*roles.GetDatabaseInRuleInRoleResponse) []lookupv1alpha1.ACLRoleDatabaseObservation {
	out := make([]lookupv1alpha1.ACLRoleDatabaseObservation, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, lookupv1alpha1.ACLRoleDatabaseObservation{
			Subscription: strconv.Itoa(redis.IntValue(item.SubscriptionId)),
			Database:     strconv.Itoa(redis.IntValue(item.DatabaseId)),
			Regions:      stringValues(item.Regions),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ObserveACLRule copies the rule id, name, and ACL syntax.
func ObserveACLRule(rule *redis_rules.GetRedisRuleResponse) lookupv1alpha1.ACLRuleObservation {
	return lookupv1alpha1.ACLRuleObservation{
		ID:   strconv.Itoa(redis.IntValue(rule.ID)),
		Name: redis.StringValue(rule.Name),
		Rule: redis.StringValue(rule.ACL),
	}
}

// ObserveACLUser copies the user id, name, and role.
func ObserveACLUser(user *users.GetUserResponse) lookupv1alpha1.ACLUserObservation {
	return lookupv1alpha1.ACLUserObservation{
		ID:   strconv.Itoa(redis.IntValue(user.ID)),
		Name: redis.StringValue(user.Name),
		Role: redis.StringValue(user.Role),
	}
}

// ObserveDataPersistence copies the catalog. An empty catalog is valid.
func ObserveDataPersistence(items []*account.DataPersistence) lookupv1alpha1.DataPersistenceObservation {
	out := make([]lookupv1alpha1.NamedDescription, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, lookupv1alpha1.NamedDescription{
			Name:        redis.StringValue(item.Name),
			Description: redis.StringValue(item.Description),
		})
	}
	if len(out) == 0 {
		out = nil
	}
	return lookupv1alpha1.DataPersistenceObservation{ID: "ALL", Options: out}
}

// ObserveDatabaseModules copies the module catalog. An empty catalog is valid.
func ObserveDatabaseModules(items []*account.DatabaseModule) lookupv1alpha1.DatabaseModulesObservation {
	out := make([]lookupv1alpha1.NamedDescription, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, lookupv1alpha1.NamedDescription{
			Name:        redis.StringValue(item.Name),
			Description: redis.StringValue(item.Description),
		})
	}
	if len(out) == 0 {
		out = nil
	}
	return lookupv1alpha1.DatabaseModulesObservation{ID: "ALL", Modules: out}
}

// EssentialsSubscriptionQuery is the filter for one Essentials subscription.
// ID "0" is unset, matching the Terraform data source.
type EssentialsSubscriptionQuery struct {
	ID   string
	Name string
}

// MatchEssentialsSubscription applies the Terraform
// rediscloud_essentials_subscription filters and returns the single match.
func MatchEssentialsSubscription(items []*subscriptions.FixedSubscriptionResponse, query EssentialsSubscriptionQuery) (*subscriptions.FixedSubscriptionResponse, error) {
	var id int
	filterID := false
	if query.ID != "" && query.ID != "0" {
		parsed, err := strconv.Atoi(query.ID)
		if err != nil {
			return nil, fmt.Errorf("subscription id %q: %w", query.ID, err)
		}
		id = parsed
		filterID = true
	}
	matched := make([]*subscriptions.FixedSubscriptionResponse, 0, 1)
	for _, item := range items {
		if item == nil {
			continue
		}
		if filterID && redis.IntValue(item.ID) != id {
			continue
		}
		if query.Name != "" && redis.StringValue(item.Name) != query.Name {
			continue
		}
		matched = append(matched, item)
	}
	return SelectOne(matched)
}

// ObserveEssentialsSubscription copies the subscription fields. A missing
// payment method id is "0", matching the Terraform data source.
func ObserveEssentialsSubscription(item *subscriptions.FixedSubscriptionResponse) lookupv1alpha1.EssentialsSubscriptionObservation {
	return lookupv1alpha1.EssentialsSubscriptionObservation{
		ID:              strconv.Itoa(redis.IntValue(item.ID)),
		Name:            redis.StringValue(item.Name),
		Status:          redis.StringValue(item.Status),
		PlanID:          strconv.Itoa(redis.IntValue(item.PlanId)),
		PaymentMethodID: strconv.Itoa(redis.IntValue(item.PaymentMethodID)),
		CreationDate:    redis.TimeValue(item.CreationDate).String(),
	}
}

// ObservePrivateLink copies the private link fields.
func ObservePrivateLink(item *privatelink.PrivateLink) lookupv1alpha1.PrivateLinkObservation {
	return lookupv1alpha1.PrivateLinkObservation{
		ResourceConfigurationArn: redis.StringValue(item.ResourceConfigurationArn),
		ResourceConfigurationID:  redis.StringValue(item.ResourceConfigurationId),
		ShareArn:                 redis.StringValue(item.ShareArn),
		ShareName:                redis.StringValue(item.ShareName),
		Principals:               observePrincipals(item.Principals),
		Connections:              observeConnections(item.Connections),
		Databases:                observePrivateLinkDatabases(item.Databases),
	}
}

func observePrincipals(items []*privatelink.PrivateLinkPrincipal) []lookupv1alpha1.PrivateLinkPrincipalObservation {
	out := make([]lookupv1alpha1.PrivateLinkPrincipalObservation, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, lookupv1alpha1.PrivateLinkPrincipalObservation{
			Principal:      redis.StringValue(item.Principal),
			PrincipalAlias: redis.StringValue(item.Alias),
			PrincipalType:  redis.StringValue(item.Type),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func observeConnections(items []*privatelink.PrivateLinkConnection) []lookupv1alpha1.PrivateLinkConnectionObservation {
	out := make([]lookupv1alpha1.PrivateLinkConnectionObservation, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, lookupv1alpha1.PrivateLinkConnectionObservation{
			AssociationDate: redis.StringValue(item.AssociationDate),
			AssociationID:   redis.StringValue(item.AssociationId),
			ConnectionID:    redis.StringValue(item.ConnectionId),
			ConnectionType:  redis.StringValue(item.Type),
			OwnerID:         redis.StringValue(item.OwnerId),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func observePrivateLinkDatabases(items []*privatelink.PrivateLinkDatabase) []lookupv1alpha1.PrivateLinkDatabaseObservation {
	out := make([]lookupv1alpha1.PrivateLinkDatabaseObservation, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, lookupv1alpha1.PrivateLinkDatabaseObservation{
			DatabaseID:           strconv.Itoa(redis.IntValue(item.DatabaseId)),
			Port:                 strconv.Itoa(redis.IntValue(item.Port)),
			ResourceLinkEndpoint: redis.StringValue(item.ResourceLinkEndpoint),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ObservePrivateServiceConnect copies the service fields. The id is the
// Terraform id, subscription/service.
func ObservePrivateServiceConnect(subscriptionID int, item *psc.PrivateServiceConnectService) (lookupv1alpha1.PrivateServiceConnectObservation, error) {
	if item == nil || item.ID == nil {
		return lookupv1alpha1.PrivateServiceConnectObservation{}, fmt.Errorf("private service connect service id is missing")
	}
	serviceID := redis.IntValue(item.ID)
	return lookupv1alpha1.PrivateServiceConnectObservation{
		ID:                             fmt.Sprintf("%d/%d", subscriptionID, serviceID),
		PrivateServiceConnectServiceID: strconv.Itoa(serviceID),
		ConnectionHostName:             redis.StringValue(item.ConnectionHostName),
		ServiceAttachmentName:          redis.StringValue(item.ServiceAttachmentName),
		Status:                         redis.StringValue(item.Status),
	}, nil
}

// EndpointScriptQuery records that Terraform always asks for the GCP
// Terraform script. The value is not a user field.
const IncludeTerraformGCPScript = true

// ObserveEndpoint skips the creation-script call for rejected and deleted
// endpoints, matching the Terraform data source.
func EndpointNeedsScript(status string) bool {
	return status != psc.EndpointStatusRejected && status != psc.EndpointStatusDeleted
}

// ObserveServiceAttachments copies the script attachments.
func ObserveServiceAttachments(script *psc.CreationScript) ([]lookupv1alpha1.ServiceAttachmentObservation, error) {
	if script == nil || script.Script == nil || script.Script.TerraformGcp == nil {
		return nil, fmt.Errorf("private service connect creation script did not include service attachments")
	}
	attachments := script.Script.TerraformGcp.ServiceAttachments
	out := make([]lookupv1alpha1.ServiceAttachmentObservation, 0, len(attachments))
	for i := range attachments {
		item := &attachments[i]
		out = append(out, lookupv1alpha1.ServiceAttachmentObservation{
			Name:               redis.StringValue(item.Name),
			DNSRecord:          redis.StringValue(item.DNSRecord),
			IPAddressName:      redis.StringValue(item.IPAddressName),
			ForwardingRuleName: redis.StringValue(item.ForwardingRuleName),
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ObserveEndpoint copies one endpoint. Attachments come from the script call.
func ObserveEndpoint(item *psc.PrivateServiceConnectEndpoint, attachments []lookupv1alpha1.ServiceAttachmentObservation) (lookupv1alpha1.PrivateServiceConnectEndpointObservation, error) {
	if item == nil || item.ID == nil {
		return lookupv1alpha1.PrivateServiceConnectEndpointObservation{}, fmt.Errorf("private service connect endpoint id is missing")
	}
	return lookupv1alpha1.PrivateServiceConnectEndpointObservation{
		PrivateServiceConnectEndpointID: strconv.Itoa(redis.IntValue(item.ID)),
		GCPProjectID:                    redis.StringValue(item.GCPProjectID),
		GCPVPCName:                      redis.StringValue(item.GCPVPCName),
		GCPVPCSubnetName:                redis.StringValue(item.GCPVPCSubnetName),
		EndpointConnectionName:          redis.StringValue(item.EndpointConnectionName),
		Status:                          redis.StringValue(item.Status),
		ServiceAttachments:              attachments,
	}, nil
}

// FilterPeerings keeps peerings with the status, or every peering when status
// is empty. The id is "ALL" or the status, matching Terraform.
func FilterPeerings(items []*prosubs.VPCPeering, status string) lookupv1alpha1.SubscriptionPeeringsObservation {
	id := "ALL"
	if status != "" {
		id = status
	}
	out := make([]lookupv1alpha1.PeeringObservation, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if status != "" && redis.StringValue(item.Status) != status {
			continue
		}
		out = append(out, observePeering(item))
	}
	if len(out) == 0 {
		out = nil
	}
	return lookupv1alpha1.SubscriptionPeeringsObservation{ID: id, Peerings: out}
}

func observePeering(item *prosubs.VPCPeering) lookupv1alpha1.PeeringObservation {
	return lookupv1alpha1.PeeringObservation{
		PeeringID:           strconv.Itoa(redis.IntValue(item.ID)),
		ProviderName:        peeringProvider(item),
		Status:              redis.StringValue(item.Status),
		AWSAccountID:        redis.StringValue(item.AWSAccountID),
		AWSPeeringID:        redis.StringValue(item.AWSPeeringID),
		VPCID:               redis.StringValue(item.VPCId),
		VPCCidr:             redis.StringValue(item.VPCCidr),
		GCPProjectID:        redis.StringValue(item.GCPProjectUID),
		GCPNetworkName:      redis.StringValue(item.NetworkName),
		GCPRedisProjectID:   redis.StringValue(item.RedisProjectUID),
		GCPRedisNetworkName: redis.StringValue(item.RedisNetworkName),
		GCPPeeringID:        redis.StringValue(item.CloudPeeringID),
		Region:              redis.StringValue(item.Region),
	}
}

func peeringProvider(item *prosubs.VPCPeering) string {
	if redis.StringValue(item.VPCId) != "" {
		return cloudProviderAWS
	}
	if redis.StringValue(item.GCPProjectUID) != "" {
		return cloudProviderGCP
	}
	return "Unknown"
}

// TransitGatewayQuery filters one attachment. Empty fields are not sent.
type TransitGatewayQuery struct {
	TGWID     string
	AWSTGWUID string
}

func transitAttachments(task *attachments.GetAttachmentsTask) ([]*attachments.TransitGatewayAttachment, error) {
	if task == nil {
		return nil, fmt.Errorf("transit gateway API returned nil task")
	}
	if task.Response == nil {
		return nil, fmt.Errorf("transit gateway API returned nil response")
	}
	if task.Response.Resource == nil {
		return nil, fmt.Errorf("transit gateway API returned nil resource")
	}
	return task.Response.Resource.TransitGatewayAttachment, nil
}

// MatchTransitGateway applies the Terraform transit gateway filters.
func MatchTransitGateway(task *attachments.GetAttachmentsTask, query TransitGatewayQuery) (*attachments.TransitGatewayAttachment, error) {
	items, err := transitAttachments(task)
	if err != nil {
		return nil, err
	}
	var tgwID int
	filterID := false
	if query.TGWID != "" {
		parsed, err := strconv.Atoi(query.TGWID)
		if err != nil {
			return nil, fmt.Errorf("tgw id %q: %w", query.TGWID, err)
		}
		tgwID = parsed
		filterID = true
	}
	matched := make([]*attachments.TransitGatewayAttachment, 0, 1)
	for _, item := range items {
		if item == nil {
			continue
		}
		if filterID && redis.IntValue(item.Id) != tgwID {
			continue
		}
		if query.AWSTGWUID != "" && redis.StringValue(item.AwsTgwUid) != query.AWSTGWUID {
			continue
		}
		matched = append(matched, item)
	}
	return SelectOne(matched)
}

// ObserveTransitGateway copies one attachment. The id is subscription/tgw.
func ObserveTransitGateway(subscriptionID int, item *attachments.TransitGatewayAttachment) lookupv1alpha1.TransitGatewayObservation {
	tgwID := redis.IntValue(item.Id)
	cidrs := make([]string, 0, len(item.Cidrs))
	for _, cidr := range item.Cidrs {
		if cidr == nil {
			continue
		}
		cidrs = append(cidrs, redis.StringValue(cidr.CidrAddress))
	}
	if len(cidrs) == 0 {
		cidrs = nil
	}
	return lookupv1alpha1.TransitGatewayObservation{
		ID:               fmt.Sprintf("%d/%d", subscriptionID, tgwID),
		TGWID:            strconv.Itoa(tgwID),
		AWSTGWUID:        redis.StringValue(item.AwsTgwUid),
		AttachmentUID:    redis.StringValue(item.AttachmentUid),
		Status:           redis.StringValue(item.Status),
		AttachmentStatus: redis.StringValue(item.AttachmentStatus),
		AWSAccountID:     redis.StringValue(item.AwsAccountId),
		CIDRs:            cidrs,
	}
}

// ObserveInvitations copies the invitation list. An empty list is valid
// when the data source is not waiting.
func ObserveInvitations(subscriptionID int, items []*attachments.TransitGatewayInvitation) lookupv1alpha1.TransitGatewayInvitationsObservation {
	out := make([]lookupv1alpha1.TransitGatewayInvitationObservation, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, lookupv1alpha1.TransitGatewayInvitationObservation{
			ID:               strconv.Itoa(redis.IntValue(item.Id)),
			Name:             redis.StringValue(item.Name),
			ResourceShareUID: redis.StringValue(item.ResourceShareUid),
			AWSAccountID:     redis.StringValue(item.AwsAccountId),
			Status:           redis.StringValue(item.Status),
			SharedDate:       redis.StringValue(item.SharedDate),
		})
	}
	if len(out) == 0 {
		out = nil
	}
	return lookupv1alpha1.TransitGatewayInvitationsObservation{
		ID:          strconv.Itoa(subscriptionID),
		Invitations: out,
	}
}

// MatchActiveActiveSubscription returns the single active-active subscription
// with the requested name.
func MatchActiveActiveSubscription(items []*prosubs.Subscription, name string) (*prosubs.Subscription, error) {
	if name == "" {
		return nil, fmt.Errorf("subscription name is required")
	}
	matched := make([]*prosubs.Subscription, 0, 1)
	for _, item := range items {
		if item == nil {
			continue
		}
		if redis.StringValue(item.DeploymentType) != prosubs.SubscriptionDeploymentTypeActiveActive {
			continue
		}
		if redis.StringValue(item.Name) != name {
			continue
		}
		matched = append(matched, item)
	}
	return SelectOne(matched)
}

// ObserveActiveActiveRegions copies the region list. The caller rejects an
// empty list, matching the Terraform data source.
func ObserveActiveActiveRegions(subscriptionID int, items []*prosubs.ActiveActiveRegion) lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation {
	out := make([]lookupv1alpha1.ActiveActiveRegionObservation, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		databases := make([]lookupv1alpha1.ActiveActiveRegionDatabaseObservation, 0, len(item.Databases))
		for i := range item.Databases {
			database := &item.Databases[i]
			databases = append(databases, lookupv1alpha1.ActiveActiveRegionDatabaseObservation{
				DatabaseID:               strconv.Itoa(redis.IntValue(database.DatabaseId)),
				DatabaseName:             redis.StringValue(database.DatabaseName),
				ReadOperationsPerSecond:  strconv.Itoa(redis.IntValue(database.ReadOperationsPerSecond)),
				WriteOperationsPerSecond: strconv.Itoa(redis.IntValue(database.WriteOperationsPerSecond)),
			})
		}
		if len(databases) == 0 {
			databases = nil
		}
		out = append(out, lookupv1alpha1.ActiveActiveRegionObservation{
			Region:                   redis.StringValue(item.Region),
			RegionID:                 strconv.Itoa(redis.IntValue(item.RegionId)),
			NetworkingDeploymentCIDR: redis.StringValue(item.DeploymentCIDR),
			VPCID:                    redis.StringValue(item.VpcId),
			Databases:                databases,
		})
	}
	return lookupv1alpha1.ActiveActiveSubscriptionRegionsObservation{
		ID:      fmt.Sprintf("%d-active-active-regions", subscriptionID),
		Regions: out,
	}
}

// ParseID parses a required integer id. It does not add a request field.
func ParseID(name, value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	id, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", name, value, err)
	}
	return id, nil
}

func stringValues(values []*string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, *value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
