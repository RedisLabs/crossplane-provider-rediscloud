/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	// Note(turkenh): we are importing this to embed provider schema document
	_ "embed"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
)

const (
	resourcePrefix = "rediscloud"
	modulePath     = "github.com/RedisLabs/provider-rediscloud"
)

//go:embed schema.json
var providerSchema string

//go:embed provider-metadata.yaml
var providerMetadata string

// GetProvider returns provider configuration
func GetProvider() *ujconfig.Provider {
	pc := ujconfig.NewProvider([]byte(providerSchema), resourcePrefix, modulePath, []byte(providerMetadata),
		ujconfig.WithRootGroup("redis.io"),
		ujconfig.WithIncludeList(ExternalNameConfigured()),
		ujconfig.WithFeaturesPackage("internal/features"),
		ujconfig.WithBasePackages(ujconfig.BasePackages{
			APIVersion: []string{"v1beta1"},
			ControllerMap: map[string]string{
				"providerconfig": ujconfig.PackageNameConfig,
			},
		}),
		ujconfig.WithDefaultResourceOptions(
			ExternalNameConfigurations(),
		))

	configure(pc)

	pc.ConfigureResources()
	// Controllers and storage move to v1beta1. v1alpha1 stays served so a
	// cluster that already stored that version can accept the CRD update.
	// Lookups stay v1alpha1. ProviderConfig stays v1beta1.
	for _, r := range pc.Resources {
		r.Version = "v1beta1"
		r.PreviousVersions = []string{"v1alpha1"}
	}
	return pc
}
