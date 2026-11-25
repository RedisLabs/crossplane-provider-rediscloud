// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"net/http"

	"github.com/pkg/errors"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"

	"github.com/RedisLabs/provider-rediscloud/internal/clients"
)

func newAPI(creds clients.Credentials) (*rediscloud.Client, error) {
	options := []rediscloud.Option{rediscloud.Auth(creds.APIKey, creds.SecretKey), rediscloud.Transporter(noRedirectTransport{next: http.DefaultTransport}), rediscloud.Logger(quietLookupLogger{})}
	if creds.URL != "" {
		options = append(options, rediscloud.BaseURL(creds.URL))
	}
	api, err := rediscloud.NewClient(options...)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create Redis Cloud API client")
	}
	return api, nil
}

// The SDK logs raw errors during asynchronous task retries, including HTTP
// response bodies. Lookup failures are reported through sanitized conditions.
type quietLookupLogger struct{}

func (quietLookupLogger) Printf(string, ...interface{}) {}
func (quietLookupLogger) Println(...interface{})        {}

// noRedirectTransport stops redirect responses before http.Client can follow
// them. The SDK adds credentials on every request, including redirected ones.
type noRedirectTransport struct{ next http.RoundTripper }

func (t noRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err == nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		_ = resp.Body.Close()
		return nil, errors.New("Redis Cloud API redirect denied")
	}
	return resp, err
}
