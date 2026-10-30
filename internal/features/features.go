/*
 Copyright 2022 Upbound Inc
*/

// Package features contains feature flags for the Redis Cloud provider.
package features

import (
	xpfeature "github.com/crossplane/crossplane-runtime/v2/pkg/feature"
)

// Feature flags.
const (
	// EnableBetaManagementPolicies enables beta support for
	// Management Policies. See the below design for more details.
	// https://github.com/crossplane/crossplane/pull/3531
	EnableBetaManagementPolicies xpfeature.Flag = xpfeature.EnableBetaManagementPolicies
)
