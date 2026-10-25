// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"strings"

	"github.com/pkg/errors"
)

// apiMessage keeps Redis Cloud HTML error pages out of status conditions.
func apiMessage(err error) string {
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "<html") {
		if i := strings.Index(msg, " - "); i >= 0 {
			return msg[:i]
		}
		return "Redis Cloud API returned an HTML error page"
	}
	const max = 512
	if len(msg) > max {
		return msg[:max]
	}
	return msg
}

func statusErr(err error) error {
	return errors.New(apiMessage(err))
}
