// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProSubscriptionObservation is the identity and current status of a Pro subscription.
type ProSubscriptionObservation struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	Status         string `json:"status,omitempty"`
	DeploymentType string `json:"deploymentType,omitempty"`
}

// ProSubscriptionSpec selects one Pro subscription by id.
type ProSubscriptionSpec struct {
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference           `json:"providerConfigRef"`
	ForProvider       SubscriptionIDParameters `json:"forProvider"`
}

// ProSubscriptionStatus is the observed Pro subscription.
type ProSubscriptionStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 ProSubscriptionObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// ProSubscription looks up the identity and status of one Pro subscription.
type ProSubscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ProSubscriptionSpec   `json:"spec"`
	Status            ProSubscriptionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProSubscriptionList contains a list of ProSubscription.
type ProSubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProSubscription `json:"items"`
}

func init() { SchemeBuilder.Register(&ProSubscription{}, &ProSubscriptionList{}) }
