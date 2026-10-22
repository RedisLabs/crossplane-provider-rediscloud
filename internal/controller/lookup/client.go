// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"github.com/pkg/errors"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"

	"github.com/RedisLabs/provider-rediscloud/internal/clients"
)

func newAPI(creds clients.Credentials) (*rediscloud.Client, error) {
	options := []rediscloud.Option{rediscloud.Auth(creds.APIKey, creds.SecretKey)}
	if creds.URL != "" {
		options = append(options, rediscloud.BaseURL(creds.URL))
	}
	api, err := rediscloud.NewClient(options...)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create Redis Cloud API client")
	}
	return api, nil
}
