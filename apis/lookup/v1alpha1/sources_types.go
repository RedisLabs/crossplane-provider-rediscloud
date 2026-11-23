// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
)

// Numeric identifiers are strings so the CRD does not use floating point.
// The controller parses them as integers before calling the API.

// ACLRoleParameters selects one ACL role by name.
type ACLRoleParameters struct {
	// Name is the role name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// ACLRoleDatabaseObservation is one database binding inside a role rule.
type ACLRoleDatabaseObservation struct {
	Subscription string   `json:"subscription,omitempty"`
	Database     string   `json:"database,omitempty"`
	Regions      []string `json:"regions,omitempty"`
}

// ACLRoleRuleObservation is one rule attached to a role.
type ACLRoleRuleObservation struct {
	Name      string                       `json:"name,omitempty"`
	Databases []ACLRoleDatabaseObservation `json:"databases,omitempty"`
}

// ACLRoleObservation is the single role that matched.
type ACLRoleObservation struct {
	ID    string                   `json:"id,omitempty"`
	Name  string                   `json:"name,omitempty"`
	Rules []ACLRoleRuleObservation `json:"rules,omitempty"`
}

// ACLRoleSpec selects one ACL role.
type ACLRoleSpec struct {
	// ProviderConfigRef names the ProviderConfig whose credentials call the API.
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference `json:"providerConfigRef"`

	// ForProvider holds the lookup filters.
	ForProvider ACLRoleParameters `json:"forProvider"`
}

// ACLRoleStatus is the observed role.
type ACLRoleStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 ACLRoleObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ACLRole looks up one Redis Cloud ACL role.
type ACLRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ACLRoleSpec   `json:"spec"`
	Status            ACLRoleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ACLRoleList contains a list of ACLRole.
type ACLRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ACLRole `json:"items"`
}

// ACLRuleParameters selects one ACL rule by name.
type ACLRuleParameters struct {
	// Name is the rule name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// ACLRuleObservation is the single rule that matched.
type ACLRuleObservation struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Rule string `json:"rule,omitempty"`
}

// ACLRuleSpec selects one ACL rule.
type ACLRuleSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference    `json:"providerConfigRef"`
	ForProvider       ACLRuleParameters `json:"forProvider"`
}

// ACLRuleStatus is the observed rule.
type ACLRuleStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 ACLRuleObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ACLRule looks up one Redis Cloud ACL rule.
type ACLRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ACLRuleSpec   `json:"spec"`
	Status            ACLRuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ACLRuleList contains a list of ACLRule.
type ACLRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ACLRule `json:"items"`
}

// ACLUserParameters selects one ACL user by name.
type ACLUserParameters struct {
	// Name is the user name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// ACLUserObservation is the single user that matched.
type ACLUserObservation struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
}

// ACLUserSpec selects one ACL user.
type ACLUserSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference    `json:"providerConfigRef"`
	ForProvider       ACLUserParameters `json:"forProvider"`
}

// ACLUserStatus is the observed user.
type ACLUserStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 ACLUserObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ACLUser looks up one Redis Cloud ACL user.
type ACLUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ACLUserSpec   `json:"spec"`
	Status            ACLUserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ACLUserList contains a list of ACLUser.
type ACLUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ACLUser `json:"items"`
}

// CloudAccountParameters filters cloud accounts.
type CloudAccountParameters struct {
	// ExcludeInternal drops the internal cloud account when true.
	// Unset does not apply the filter.
	// +optional
	ExcludeInternalAccount *bool `json:"excludeInternalAccount,omitempty"`

	// ProviderType is the cloud provider, such as AWS or GCP.
	// +optional
	ProviderType *string `json:"providerType,omitempty"`

	// Name is the cloud account name.
	// +optional
	Name *string `json:"name,omitempty"`
}

// CloudAccountObservation is the single cloud account that matched.
type CloudAccountObservation struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	AccessKeyID  string `json:"accessKeyId,omitempty"`
	ProviderType string `json:"providerType,omitempty"`
}

// CloudAccountSpec selects one cloud account.
type CloudAccountSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference         `json:"providerConfigRef"`
	ForProvider       CloudAccountParameters `json:"forProvider,omitempty"`
}

// CloudAccountStatus is the observed cloud account.
type CloudAccountStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 CloudAccountObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// CloudAccount looks up one Redis Cloud cloud account.
type CloudAccount struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CloudAccountSpec   `json:"spec"`
	Status            CloudAccountStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudAccountList contains a list of CloudAccount.
type CloudAccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudAccount `json:"items"`
}

// NamedDescription is a catalog entry with a name and a description.
type NamedDescription struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// DataPersistenceObservation is the data persistence catalog.
type DataPersistenceObservation struct {
	ID      string             `json:"id,omitempty"`
	Options []NamedDescription `json:"options,omitempty"`
}

// DataPersistenceSpec reads the data persistence catalog.
type DataPersistenceSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference `json:"providerConfigRef"`
}

// DataPersistenceStatus is the observed catalog.
type DataPersistenceStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 DataPersistenceObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// DataPersistence looks up Redis Cloud data persistence options.
type DataPersistence struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DataPersistenceSpec   `json:"spec"`
	Status            DataPersistenceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DataPersistenceList contains a list of DataPersistence.
type DataPersistenceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataPersistence `json:"items"`
}

// DatabaseModulesObservation is the database module catalog.
type DatabaseModulesObservation struct {
	ID      string             `json:"id,omitempty"`
	Modules []NamedDescription `json:"modules,omitempty"`
}

// DatabaseModulesSpec reads the database module catalog.
type DatabaseModulesSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference `json:"providerConfigRef"`
}

// DatabaseModulesStatus is the observed catalog.
type DatabaseModulesStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 DatabaseModulesObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// DatabaseModules looks up Redis Cloud database modules.
type DatabaseModules struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabaseModulesSpec   `json:"spec"`
	Status            DatabaseModulesStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseModulesList contains a list of DatabaseModules.
type DatabaseModulesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseModules `json:"items"`
}

// EssentialsSubscriptionParameters filters Essentials subscriptions.
type EssentialsSubscriptionParameters struct {
	// ID is the subscription id. "0" matches every id.
	// +optional
	ID *string `json:"id,omitempty"`

	// Name is the subscription name.
	// +optional
	Name *string `json:"name,omitempty"`
}

// EssentialsSubscriptionObservation is the single subscription that matched.
type EssentialsSubscriptionObservation struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	Status          string `json:"status,omitempty"`
	PlanID          string `json:"planId,omitempty"`
	PaymentMethodID string `json:"paymentMethodId,omitempty"`
	CreationDate    string `json:"creationDate,omitempty"`
}

// EssentialsSubscriptionSpec selects one Essentials subscription.
type EssentialsSubscriptionSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference                   `json:"providerConfigRef"`
	ForProvider       EssentialsSubscriptionParameters `json:"forProvider,omitempty"`
}

// EssentialsSubscriptionStatus is the observed subscription.
type EssentialsSubscriptionStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 EssentialsSubscriptionObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// EssentialsSubscription looks up one Redis Cloud Essentials subscription.
type EssentialsSubscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              EssentialsSubscriptionSpec   `json:"spec"`
	Status            EssentialsSubscriptionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EssentialsSubscriptionList contains a list of EssentialsSubscription.
type EssentialsSubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EssentialsSubscription `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&ACLRole{}, &ACLRoleList{},
		&ACLRule{}, &ACLRuleList{},
		&ACLUser{}, &ACLUserList{},
		&CloudAccount{}, &CloudAccountList{},
		&DataPersistence{}, &DataPersistenceList{},
		&DatabaseModules{}, &DatabaseModulesList{},
		&EssentialsSubscription{}, &EssentialsSubscriptionList{},
	)
}
