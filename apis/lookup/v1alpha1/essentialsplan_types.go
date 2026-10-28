// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
)

// EssentialsPlanParameters filters Essentials plans the way the
// rediscloud_essentials_plan Terraform data source does.
// Size and identifier fields are strings so the CRD does not use floating
// point. The controller parses them as numbers.
type EssentialsPlanParameters struct {
	// ID is the plan id, when the caller already knows it.
	// +optional
	ID *string `json:"id,omitempty"`

	// Name is the plan name. It is not unique across providers.
	// +optional
	Name *string `json:"name,omitempty"`

	// Size is the plan capacity, in sizeMeasurementUnit.
	// +optional
	Size *string `json:"size,omitempty"`

	// SizeMeasurementUnit is the unit of Size, usually MB or GB.
	// +optional
	SizeMeasurementUnit *string `json:"sizeMeasurementUnit,omitempty"`

	// CloudProvider is AWS, GCP, or Azure.
	// +optional
	CloudProvider *string `json:"cloudProvider,omitempty"`

	// Region is the cloud region, such as us-central1.
	// +optional
	Region *string `json:"region,omitempty"`

	// Availability is No replication, Single-zone, or Multi-zone.
	// +optional
	Availability *string `json:"availability,omitempty"`

	// SupportDataPersistence filters on data persistence support.
	// +optional
	SupportDataPersistence *bool `json:"supportDataPersistence,omitempty"`

	// SupportReplication filters on replication support.
	// +optional
	SupportReplication *bool `json:"supportReplication,omitempty"`

	// SubscriptionID limits the list to plans available for that subscription.
	// +optional
	SubscriptionID *string `json:"subscriptionId,omitempty"`
}

// EssentialsPlanObservation is the single plan that matched.
type EssentialsPlanObservation struct {
	ID                  string `json:"id,omitempty"`
	Name                string `json:"name,omitempty"`
	Size                string `json:"size,omitempty"`
	SizeMeasurementUnit string `json:"sizeMeasurementUnit,omitempty"`
	CloudProvider       string `json:"cloudProvider,omitempty"`
	Region              string `json:"region,omitempty"`
	RegionID            string `json:"regionId,omitempty"`
	Availability        string `json:"availability,omitempty"`
	Price               string `json:"price,omitempty"`
	PriceCurrency       string `json:"priceCurrency,omitempty"`
	PricePeriod         string `json:"pricePeriod,omitempty"`
}

// EssentialsPlanSpec selects one Essentials plan.
type EssentialsPlanSpec struct {
	// ProviderConfigRef names the ProviderConfig whose credentials call the API.
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference `json:"providerConfigRef"`

	// ForProvider holds the lookup filters.
	ForProvider EssentialsPlanParameters `json:"forProvider,omitempty"`
}

// EssentialsPlanStatus is the observed plan.
type EssentialsPlanStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`

	// AtProvider is the matched plan.
	AtProvider EssentialsPlanObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// EssentialsPlan looks up one Redis Cloud Essentials plan.
type EssentialsPlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EssentialsPlanSpec   `json:"spec"`
	Status EssentialsPlanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EssentialsPlanList contains a list of EssentialsPlan.
type EssentialsPlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EssentialsPlan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EssentialsPlan{}, &EssentialsPlanList{})
}
