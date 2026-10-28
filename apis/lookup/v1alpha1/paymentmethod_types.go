// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
)

// PaymentMethodParameters filters payment methods the way the
// rediscloud_payment_method Terraform data source does.
type PaymentMethodParameters struct {
	// CardType is the card brand, such as Visa. Empty matches every brand.
	// +optional
	CardType *string `json:"cardType,omitempty"`

	// LastFourNumbers is the last four digits of the card, zero-padded.
	// Empty matches every card.
	// +optional
	LastFourNumbers *string `json:"lastFourNumbers,omitempty"`

	// ExcludeExpired drops cards whose expiration month is before the
	// current month. Defaults to true when unset.
	// +optional
	ExcludeExpired *bool `json:"excludeExpired,omitempty"`
}

// PaymentMethodObservation is the single payment method that matched.
type PaymentMethodObservation struct {
	// ID is the payment method id to copy onto a subscription.
	ID string `json:"id,omitempty"`

	// CardType is the card brand returned by the API.
	CardType string `json:"cardType,omitempty"`

	// LastFourNumbers is the last four digits, zero-padded.
	LastFourNumbers string `json:"lastFourNumbers,omitempty"`
}

// PaymentMethodSpec selects one payment method.
type PaymentMethodSpec struct {
	// ProviderConfigRef names the ProviderConfig whose credentials call the API.
	// +kubebuilder:validation:Required
	ProviderConfigRef xpv1.Reference `json:"providerConfigRef"`

	// ForProvider holds the lookup filters.
	ForProvider PaymentMethodParameters `json:"forProvider,omitempty"`
}

// PaymentMethodStatus is the observed payment method.
type PaymentMethodStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`

	// AtProvider is the matched payment method.
	AtProvider PaymentMethodObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.atProvider.id"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,rediscloud}

// PaymentMethod looks up one Redis Cloud payment method.
type PaymentMethod struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PaymentMethodSpec   `json:"spec"`
	Status PaymentMethodStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PaymentMethodList contains a list of PaymentMethod.
type PaymentMethodList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PaymentMethod `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PaymentMethod{}, &PaymentMethodList{})
}
