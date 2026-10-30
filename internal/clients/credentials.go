// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package clients

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	"github.com/RedisLabs/provider-rediscloud/apis/v1beta1"
)

// Credentials are the Redis Cloud API values stored on a ProviderConfig.
type Credentials struct {
	APIKey    string
	SecretKey string
	URL       string
}

// LoadCredentials reads api_key, secret_key, and the optional url from the
// named ProviderConfig. The values are not logged.
func LoadCredentials(ctx context.Context, kube client.Client, ref xpv1.Reference) (Credentials, error) {
	var out Credentials
	if ref.Name == "" {
		return out, errors.New(errNoProviderConfig)
	}
	pc := &v1beta1.ProviderConfig{}
	if err := kube.Get(ctx, types.NamespacedName{Name: ref.Name}, pc); err != nil {
		return out, errors.Wrap(err, errGetProviderConfig)
	}
	data, err := resource.CommonCredentialExtractor(ctx, pc.Spec.Credentials.Source, kube, pc.Spec.Credentials.CommonCredentialSelectors)
	if err != nil {
		return out, errors.Wrap(err, errExtractCredentials)
	}
	parsed := map[string]string{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return out, errors.Wrap(err, errUnmarshalCredentials)
	}
	out.APIKey = parsed["api_key"]
	out.SecretKey = parsed["secret_key"]
	out.URL = parsed["url"]
	if out.APIKey == "" || out.SecretKey == "" {
		return out, errors.New("provider credentials require api_key and secret_key")
	}
	return out, nil
}
