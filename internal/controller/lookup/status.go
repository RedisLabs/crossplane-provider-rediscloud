// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import "github.com/pkg/errors"

// statusErr intentionally does not copy arbitrary errors to Kubernetes status.
// SDK, transport, and credential errors can contain response bodies or secrets.
func statusErr(_ error) error {
	return errors.New("lookup failed; check provider configuration and Redis Cloud access")
}
