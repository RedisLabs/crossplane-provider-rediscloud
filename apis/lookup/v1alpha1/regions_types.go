// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// RegionsParameters filters regions the way the rediscloud_regions Terraform
// data source does, plus an optional region name.
type RegionsParameters struct {
	// ProviderName is AWS or GCP. Empty returns every provider the API lists.
	// +optional
	// +kubebuilder:validation:Enum=AWS;GCP
	ProviderName *string `json:"providerName,omitempty"`

	// Name is the cloud region identifier, such as us-central1.
	// Empty returns every region for ProviderName.
	// +optional
	Name *string `json:"name,omitempty"`
}

// RegionObservation is one region from the API.
type RegionObservation struct {
	// RegionID is the Redis Cloud region id.
	RegionID string `json:"regionId,omitempty"`

	// Name is the cloud provider region name.
	Name string `json:"name,omitempty"`

	// ProviderName is AWS or GCP.
	ProviderName string `json:"providerName,omitempty"`
}

// RegionsSpec selects a set of regions.
type RegionsSpec struct {
	// ProviderConfigRef names the ProviderConfig whose credentials call the API.
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference `json:"providerConfigRef"`

	// ForProvider holds the lookup filters.
	ForProvider RegionsParameters `json:"forProvider,omitempty"`
}

// RegionsStatus is the observed region list.
type RegionsStatus struct {
	xpv1.ResourceStatus `json:",inline"`

	// AtProvider holds the regions that matched.
	AtProvider RegionsObservation `json:"atProvider,omitempty"`
}

// RegionsObservation is the matched region list.
type RegionsObservation struct {
	// Regions is every region that passed the filter.
	Regions []RegionObservation `json:"regions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// Regions looks up Redis Cloud regions.
type Regions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RegionsSpec   `json:"spec"`
	Status RegionsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RegionsList contains a list of Regions.
type RegionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Regions `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Regions{}, &RegionsList{})
}
