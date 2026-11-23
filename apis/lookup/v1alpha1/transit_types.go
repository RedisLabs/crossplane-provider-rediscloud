// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
)

// TransitGatewayParameters selects one transit gateway attachment.
// The Terraform wait timeout is not a request field and is not accepted here.
type TransitGatewayParameters struct {
	// SubscriptionID is the Pro subscription id.
	// +kubebuilder:validation:Required
	SubscriptionID string `json:"subscriptionId"`

	// TGWID is the transit gateway id. Empty matches every id.
	// +optional
	TGWID *string `json:"tgwId,omitempty"`

	// AWSTGWUID is the AWS transit gateway id. Empty matches every id.
	// +optional
	AWSTGWUID *string `json:"awsTgwUid,omitempty"`
}

// ActiveActiveTransitGatewayParameters selects one Active-Active attachment.
type ActiveActiveTransitGatewayParameters struct {
	// SubscriptionID is the Active-Active subscription id.
	// +kubebuilder:validation:Required
	SubscriptionID string `json:"subscriptionId"`

	// RegionID is the region id.
	// +kubebuilder:validation:Required
	RegionID string `json:"regionId"`

	// TGWID is the transit gateway id. Empty matches every id.
	// +optional
	TGWID *string `json:"tgwId,omitempty"`

	// AWSTGWUID is the AWS transit gateway id. Empty matches every id.
	// +optional
	AWSTGWUID *string `json:"awsTgwUid,omitempty"`
}

// TransitGatewayObservation is the single attachment that matched.
type TransitGatewayObservation struct {
	ID               string   `json:"id,omitempty"`
	TGWID            string   `json:"tgwId,omitempty"`
	AWSTGWUID        string   `json:"awsTgwUid,omitempty"`
	AttachmentUID    string   `json:"attachmentUid,omitempty"`
	Status           string   `json:"status,omitempty"`
	AttachmentStatus string   `json:"attachmentStatus,omitempty"`
	AWSAccountID     string   `json:"awsAccountId,omitempty"`
	CIDRs            []string `json:"cidrs,omitempty"`
}

// TransitGatewaySpec selects one Pro transit gateway.
type TransitGatewaySpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference           `json:"providerConfigRef"`
	ForProvider       TransitGatewayParameters `json:"forProvider"`
}

// TransitGatewayStatus is the observed attachment.
type TransitGatewayStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 TransitGatewayObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// TransitGateway looks up one transit gateway attachment.
type TransitGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TransitGatewaySpec   `json:"spec"`
	Status            TransitGatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TransitGatewayList contains a list of TransitGateway.
type TransitGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TransitGateway `json:"items"`
}

// ActiveActiveTransitGatewaySpec selects one Active-Active attachment.
type ActiveActiveTransitGatewaySpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference                       `json:"providerConfigRef"`
	ForProvider       ActiveActiveTransitGatewayParameters `json:"forProvider"`
}

// ActiveActiveTransitGatewayStatus is the observed attachment.
type ActiveActiveTransitGatewayStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 TransitGatewayObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ActiveActiveTransitGateway looks up one Active-Active transit gateway attachment.
type ActiveActiveTransitGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ActiveActiveTransitGatewaySpec   `json:"spec"`
	Status            ActiveActiveTransitGatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveActiveTransitGatewayList contains a list of ActiveActiveTransitGateway.
type ActiveActiveTransitGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveActiveTransitGateway `json:"items"`
}

// TransitGatewayInvitationObservation is one invitation.
type TransitGatewayInvitationObservation struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name,omitempty"`
	ResourceShareUID string `json:"resourceShareUid,omitempty"`
	AWSAccountID     string `json:"awsAccountId,omitempty"`
	Status           string `json:"status,omitempty"`
	SharedDate       string `json:"sharedDate,omitempty"`
}

// TransitGatewayInvitationsObservation is the invitation list.
type TransitGatewayInvitationsObservation struct {
	ID          string                                `json:"id,omitempty"`
	Invitations []TransitGatewayInvitationObservation `json:"invitations,omitempty"`
}

// TransitGatewayInvitationsSpec reads Pro transit gateway invitations.
type TransitGatewayInvitationsSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference           `json:"providerConfigRef"`
	ForProvider       SubscriptionIDParameters `json:"forProvider"`
}

// TransitGatewayInvitationsStatus is the observed invitation list.
type TransitGatewayInvitationsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 TransitGatewayInvitationsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// TransitGatewayInvitations looks up transit gateway invitations.
type TransitGatewayInvitations struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TransitGatewayInvitationsSpec   `json:"spec"`
	Status            TransitGatewayInvitationsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TransitGatewayInvitationsList contains a list of TransitGatewayInvitations.
type TransitGatewayInvitationsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TransitGatewayInvitations `json:"items"`
}

// ActiveActiveTransitGatewayInvitationsSpec reads Active-Active invitations.
type ActiveActiveTransitGatewayInvitationsSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference               `json:"providerConfigRef"`
	ForProvider       SubscriptionRegionParameters `json:"forProvider"`
}

// ActiveActiveTransitGatewayInvitationsStatus is the observed invitation list.
type ActiveActiveTransitGatewayInvitationsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 TransitGatewayInvitationsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ActiveActiveTransitGatewayInvitations looks up Active-Active transit gateway invitations.
type ActiveActiveTransitGatewayInvitations struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ActiveActiveTransitGatewayInvitationsSpec   `json:"spec"`
	Status            ActiveActiveTransitGatewayInvitationsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveActiveTransitGatewayInvitationsList contains a list of ActiveActiveTransitGatewayInvitations.
type ActiveActiveTransitGatewayInvitationsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveActiveTransitGatewayInvitations `json:"items"`
}

// ActiveActiveRegionDatabaseObservation is one database in a region.
type ActiveActiveRegionDatabaseObservation struct {
	DatabaseID               string `json:"databaseId,omitempty"`
	DatabaseName             string `json:"databaseName,omitempty"`
	ReadOperationsPerSecond  string `json:"readOperationsPerSecond,omitempty"`
	WriteOperationsPerSecond string `json:"writeOperationsPerSecond,omitempty"`
}

// ActiveActiveRegionObservation is one Active-Active region.
type ActiveActiveRegionObservation struct {
	Region                   string                                  `json:"region,omitempty"`
	RegionID                 string                                  `json:"regionId,omitempty"`
	NetworkingDeploymentCIDR string                                  `json:"networkingDeploymentCidr,omitempty"`
	VPCID                    string                                  `json:"vpcId,omitempty"`
	Databases                []ActiveActiveRegionDatabaseObservation `json:"databases,omitempty"`
}

// ActiveActiveSubscriptionRegionsParameters selects one Active-Active subscription by name.
type ActiveActiveSubscriptionRegionsParameters struct {
	// SubscriptionName is the subscription name.
	// +kubebuilder:validation:Required
	SubscriptionName string `json:"subscriptionName"`
}

// ActiveActiveSubscriptionRegionsObservation is the region list.
type ActiveActiveSubscriptionRegionsObservation struct {
	ID      string                          `json:"id,omitempty"`
	Regions []ActiveActiveRegionObservation `json:"regions,omitempty"`
}

// ActiveActiveSubscriptionRegionsSpec reads regions for one subscription.
type ActiveActiveSubscriptionRegionsSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference                            `json:"providerConfigRef"`
	ForProvider       ActiveActiveSubscriptionRegionsParameters `json:"forProvider"`
}

// ActiveActiveSubscriptionRegionsStatus is the observed region list.
type ActiveActiveSubscriptionRegionsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 ActiveActiveSubscriptionRegionsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ActiveActiveSubscriptionRegions looks up regions on one Active-Active subscription.
type ActiveActiveSubscriptionRegions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ActiveActiveSubscriptionRegionsSpec   `json:"spec"`
	Status            ActiveActiveSubscriptionRegionsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveActiveSubscriptionRegionsList contains a list of ActiveActiveSubscriptionRegions.
type ActiveActiveSubscriptionRegionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveActiveSubscriptionRegions `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&TransitGateway{}, &TransitGatewayList{},
		&ActiveActiveTransitGateway{}, &ActiveActiveTransitGatewayList{},
		&TransitGatewayInvitations{}, &TransitGatewayInvitationsList{},
		&ActiveActiveTransitGatewayInvitations{}, &ActiveActiveTransitGatewayInvitationsList{},
		&ActiveActiveSubscriptionRegions{}, &ActiveActiveSubscriptionRegionsList{},
	)
}
