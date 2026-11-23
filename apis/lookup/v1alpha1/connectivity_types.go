// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
)

// SubscriptionIDParameters is the subscription id sent by a data source.
type SubscriptionIDParameters struct {
	// SubscriptionID is the Pro subscription id.
	// +kubebuilder:validation:Required
	SubscriptionID string `json:"subscriptionId"`
}

// SubscriptionRegionParameters adds the region id sent by an Active-Active data source.
type SubscriptionRegionParameters struct {
	// SubscriptionID is the Active-Active subscription id.
	// +kubebuilder:validation:Required
	SubscriptionID string `json:"subscriptionId"`

	// RegionID is the region id inside that subscription.
	// +kubebuilder:validation:Required
	RegionID string `json:"regionId"`
}

// PrivateLinkPrincipalObservation is one private link principal.
type PrivateLinkPrincipalObservation struct {
	Principal      string `json:"principal,omitempty"`
	PrincipalAlias string `json:"principalAlias,omitempty"`
	PrincipalType  string `json:"principalType,omitempty"`
}

// PrivateLinkConnectionObservation is one private link connection.
type PrivateLinkConnectionObservation struct {
	AssociationDate string `json:"associationDate,omitempty"`
	AssociationID   string `json:"associationId,omitempty"`
	ConnectionID    string `json:"connectionId,omitempty"`
	ConnectionType  string `json:"connectionType,omitempty"`
	OwnerID         string `json:"ownerId,omitempty"`
}

// PrivateLinkDatabaseObservation is one database exposed by a private link.
type PrivateLinkDatabaseObservation struct {
	DatabaseID           string `json:"databaseId,omitempty"`
	Port                 string `json:"port,omitempty"`
	ResourceLinkEndpoint string `json:"resourceLinkEndpoint,omitempty"`
}

// PrivateLinkObservation is one private link.
type PrivateLinkObservation struct {
	ResourceConfigurationArn string                             `json:"resourceConfigurationArn,omitempty"`
	ResourceConfigurationID  string                             `json:"resourceConfigurationId,omitempty"`
	ShareArn                 string                             `json:"shareArn,omitempty"`
	ShareName                string                             `json:"shareName,omitempty"`
	Principals               []PrivateLinkPrincipalObservation  `json:"principals,omitempty"`
	Connections              []PrivateLinkConnectionObservation `json:"connections,omitempty"`
	Databases                []PrivateLinkDatabaseObservation   `json:"databases,omitempty"`
}

// PrivateLinkSpec reads one Pro private link.
type PrivateLinkSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference           `json:"providerConfigRef"`
	ForProvider       SubscriptionIDParameters `json:"forProvider"`
}

// PrivateLinkStatus is the observed private link.
type PrivateLinkStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateLinkObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// PrivateLink looks up one Redis Cloud private link.
type PrivateLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PrivateLinkSpec   `json:"spec"`
	Status            PrivateLinkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PrivateLinkList contains a list of PrivateLink.
type PrivateLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrivateLink `json:"items"`
}

// ActiveActivePrivateLinkSpec reads one Active-Active private link.
type ActiveActivePrivateLinkSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference               `json:"providerConfigRef"`
	ForProvider       SubscriptionRegionParameters `json:"forProvider"`
}

// ActiveActivePrivateLinkStatus is the observed private link.
type ActiveActivePrivateLinkStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateLinkObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ActiveActivePrivateLink looks up one Active-Active private link.
type ActiveActivePrivateLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ActiveActivePrivateLinkSpec   `json:"spec"`
	Status            ActiveActivePrivateLinkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveActivePrivateLinkList contains a list of ActiveActivePrivateLink.
type ActiveActivePrivateLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveActivePrivateLink `json:"items"`
}

// PrivateLinkEndpointScriptObservation is the endpoint script.
type PrivateLinkEndpointScriptObservation struct {
	ID             string `json:"id,omitempty"`
	EndpointScript string `json:"endpointScript,omitempty"`
}

// PrivateLinkEndpointScriptSpec reads one Pro private link endpoint script.
type PrivateLinkEndpointScriptSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference           `json:"providerConfigRef"`
	ForProvider       SubscriptionIDParameters `json:"forProvider"`
}

// PrivateLinkEndpointScriptStatus is the observed script.
type PrivateLinkEndpointScriptStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateLinkEndpointScriptObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// PrivateLinkEndpointScript looks up one private link endpoint script.
type PrivateLinkEndpointScript struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PrivateLinkEndpointScriptSpec   `json:"spec"`
	Status            PrivateLinkEndpointScriptStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PrivateLinkEndpointScriptList contains a list of PrivateLinkEndpointScript.
type PrivateLinkEndpointScriptList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrivateLinkEndpointScript `json:"items"`
}

// ActiveActivePrivateLinkEndpointScriptSpec reads one Active-Active script.
type ActiveActivePrivateLinkEndpointScriptSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference               `json:"providerConfigRef"`
	ForProvider       SubscriptionRegionParameters `json:"forProvider"`
}

// ActiveActivePrivateLinkEndpointScriptStatus is the observed script.
type ActiveActivePrivateLinkEndpointScriptStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateLinkEndpointScriptObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ActiveActivePrivateLinkEndpointScript looks up one Active-Active private link endpoint script.
type ActiveActivePrivateLinkEndpointScript struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ActiveActivePrivateLinkEndpointScriptSpec   `json:"spec"`
	Status            ActiveActivePrivateLinkEndpointScriptStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveActivePrivateLinkEndpointScriptList contains a list of ActiveActivePrivateLinkEndpointScript.
type ActiveActivePrivateLinkEndpointScriptList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveActivePrivateLinkEndpointScript `json:"items"`
}

// PrivateServiceConnectObservation is one private service connect service.
type PrivateServiceConnectObservation struct {
	ID                             string `json:"id,omitempty"`
	PrivateServiceConnectServiceID string `json:"privateServiceConnectServiceId,omitempty"`
	ConnectionHostName             string `json:"connectionHostName,omitempty"`
	ServiceAttachmentName          string `json:"serviceAttachmentName,omitempty"`
	Status                         string `json:"status,omitempty"`
}

// PrivateServiceConnectSpec reads one Pro private service connect service.
type PrivateServiceConnectSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference           `json:"providerConfigRef"`
	ForProvider       SubscriptionIDParameters `json:"forProvider"`
}

// PrivateServiceConnectStatus is the observed service.
type PrivateServiceConnectStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateServiceConnectObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// PrivateServiceConnect looks up one private service connect service.
type PrivateServiceConnect struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PrivateServiceConnectSpec   `json:"spec"`
	Status            PrivateServiceConnectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PrivateServiceConnectList contains a list of PrivateServiceConnect.
type PrivateServiceConnectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrivateServiceConnect `json:"items"`
}

// ActiveActivePrivateServiceConnectSpec reads one Active-Active service.
type ActiveActivePrivateServiceConnectSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference               `json:"providerConfigRef"`
	ForProvider       SubscriptionRegionParameters `json:"forProvider"`
}

// ActiveActivePrivateServiceConnectStatus is the observed service.
type ActiveActivePrivateServiceConnectStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateServiceConnectObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ActiveActivePrivateServiceConnect looks up one Active-Active private service connect service.
type ActiveActivePrivateServiceConnect struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ActiveActivePrivateServiceConnectSpec   `json:"spec"`
	Status            ActiveActivePrivateServiceConnectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveActivePrivateServiceConnectList contains a list of ActiveActivePrivateServiceConnect.
type ActiveActivePrivateServiceConnectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveActivePrivateServiceConnect `json:"items"`
}

// ServiceAttachmentObservation is one service attachment from the creation script.
type ServiceAttachmentObservation struct {
	Name               string `json:"name,omitempty"`
	DNSRecord          string `json:"dnsRecord,omitempty"`
	IPAddressName      string `json:"ipAddressName,omitempty"`
	ForwardingRuleName string `json:"forwardingRuleName,omitempty"`
}

// PrivateServiceConnectEndpointObservation is one endpoint.
type PrivateServiceConnectEndpointObservation struct {
	PrivateServiceConnectEndpointID string                         `json:"privateServiceConnectEndpointId,omitempty"`
	GCPProjectID                    string                         `json:"gcpProjectId,omitempty"`
	GCPVPCName                      string                         `json:"gcpVpcName,omitempty"`
	GCPVPCSubnetName                string                         `json:"gcpVpcSubnetName,omitempty"`
	EndpointConnectionName          string                         `json:"endpointConnectionName,omitempty"`
	Status                          string                         `json:"status,omitempty"`
	ServiceAttachments              []ServiceAttachmentObservation `json:"serviceAttachments,omitempty"`
}

// PrivateServiceConnectEndpointsObservation is the endpoint list.
type PrivateServiceConnectEndpointsObservation struct {
	ID        string                                     `json:"id,omitempty"`
	Endpoints []PrivateServiceConnectEndpointObservation `json:"endpoints,omitempty"`
}

// PrivateServiceConnectEndpointsParameters selects endpoints on one service.
type PrivateServiceConnectEndpointsParameters struct {
	// SubscriptionID is the Pro subscription id.
	// +kubebuilder:validation:Required
	SubscriptionID string `json:"subscriptionId"`

	// PrivateServiceConnectServiceID is the service id.
	// +kubebuilder:validation:Required
	PrivateServiceConnectServiceID string `json:"privateServiceConnectServiceId"`
}

// PrivateServiceConnectEndpointsSpec reads Pro private service connect endpoints.
type PrivateServiceConnectEndpointsSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference                           `json:"providerConfigRef"`
	ForProvider       PrivateServiceConnectEndpointsParameters `json:"forProvider"`
}

// PrivateServiceConnectEndpointsStatus is the observed endpoint list.
type PrivateServiceConnectEndpointsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateServiceConnectEndpointsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// PrivateServiceConnectEndpoints looks up private service connect endpoints.
type PrivateServiceConnectEndpoints struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PrivateServiceConnectEndpointsSpec   `json:"spec"`
	Status            PrivateServiceConnectEndpointsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PrivateServiceConnectEndpointsList contains a list of PrivateServiceConnectEndpoints.
type PrivateServiceConnectEndpointsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrivateServiceConnectEndpoints `json:"items"`
}

// ActiveActivePrivateServiceConnectEndpointsParameters selects Active-Active endpoints.
type ActiveActivePrivateServiceConnectEndpointsParameters struct {
	// SubscriptionID is the Active-Active subscription id.
	// +kubebuilder:validation:Required
	SubscriptionID string `json:"subscriptionId"`

	// RegionID is the region id.
	// +kubebuilder:validation:Required
	RegionID string `json:"regionId"`

	// PrivateServiceConnectServiceID is the service id.
	// +kubebuilder:validation:Required
	PrivateServiceConnectServiceID string `json:"privateServiceConnectServiceId"`
}

// ActiveActivePrivateServiceConnectEndpointsSpec reads Active-Active endpoints.
type ActiveActivePrivateServiceConnectEndpointsSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference                                       `json:"providerConfigRef"`
	ForProvider       ActiveActivePrivateServiceConnectEndpointsParameters `json:"forProvider"`
}

// ActiveActivePrivateServiceConnectEndpointsStatus is the observed endpoint list.
type ActiveActivePrivateServiceConnectEndpointsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 PrivateServiceConnectEndpointsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ActiveActivePrivateServiceConnectEndpoints looks up Active-Active private service connect endpoints.
type ActiveActivePrivateServiceConnectEndpoints struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ActiveActivePrivateServiceConnectEndpointsSpec   `json:"spec"`
	Status            ActiveActivePrivateServiceConnectEndpointsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveActivePrivateServiceConnectEndpointsList contains a list of ActiveActivePrivateServiceConnectEndpoints.
type ActiveActivePrivateServiceConnectEndpointsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveActivePrivateServiceConnectEndpoints `json:"items"`
}

// PeeringObservation is one VPC peering.
type PeeringObservation struct {
	PeeringID           string `json:"peeringId,omitempty"`
	ProviderName        string `json:"providerName,omitempty"`
	Status              string `json:"status,omitempty"`
	AWSAccountID        string `json:"awsAccountId,omitempty"`
	AWSPeeringID        string `json:"awsPeeringId,omitempty"`
	VPCID               string `json:"vpcId,omitempty"`
	VPCCidr             string `json:"vpcCidr,omitempty"`
	GCPProjectID        string `json:"gcpProjectId,omitempty"`
	GCPNetworkName      string `json:"gcpNetworkName,omitempty"`
	GCPRedisProjectID   string `json:"gcpRedisProjectId,omitempty"`
	GCPRedisNetworkName string `json:"gcpRedisNetworkName,omitempty"`
	GCPPeeringID        string `json:"gcpPeeringId,omitempty"`
	Region              string `json:"region,omitempty"`
}

// SubscriptionPeeringsParameters selects peerings on one subscription.
type SubscriptionPeeringsParameters struct {
	// SubscriptionID is the Pro subscription id.
	// +kubebuilder:validation:Required
	SubscriptionID string `json:"subscriptionId"`

	// Status filters peerings. Empty returns every peering.
	// +optional
	// +kubebuilder:validation:Enum=initiating-request;pending-acceptance;active;inactive;failed
	Status *string `json:"status,omitempty"`
}

// SubscriptionPeeringsObservation is the peering list.
type SubscriptionPeeringsObservation struct {
	ID       string               `json:"id,omitempty"`
	Peerings []PeeringObservation `json:"peerings,omitempty"`
}

// SubscriptionPeeringsSpec reads subscription peerings.
type SubscriptionPeeringsSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference                 `json:"providerConfigRef"`
	ForProvider       SubscriptionPeeringsParameters `json:"forProvider"`
}

// SubscriptionPeeringsStatus is the observed peering list.
type SubscriptionPeeringsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 SubscriptionPeeringsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// SubscriptionPeerings looks up VPC peerings on one subscription.
type SubscriptionPeerings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SubscriptionPeeringsSpec   `json:"spec"`
	Status            SubscriptionPeeringsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SubscriptionPeeringsList contains a list of SubscriptionPeerings.
type SubscriptionPeeringsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SubscriptionPeerings `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&PrivateLink{}, &PrivateLinkList{},
		&ActiveActivePrivateLink{}, &ActiveActivePrivateLinkList{},
		&PrivateLinkEndpointScript{}, &PrivateLinkEndpointScriptList{},
		&ActiveActivePrivateLinkEndpointScript{}, &ActiveActivePrivateLinkEndpointScriptList{},
		&PrivateServiceConnect{}, &PrivateServiceConnectList{},
		&ActiveActivePrivateServiceConnect{}, &ActiveActivePrivateServiceConnectList{},
		&PrivateServiceConnectEndpoints{}, &PrivateServiceConnectEndpointsList{},
		&ActiveActivePrivateServiceConnectEndpoints{}, &ActiveActivePrivateServiceConnectEndpointsList{},
		&SubscriptionPeerings{}, &SubscriptionPeeringsList{},
	)
}
