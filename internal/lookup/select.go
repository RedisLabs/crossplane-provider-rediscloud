// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Package lookup filters Redis Cloud API lists the way the Terraform data
// sources do.
package lookup

import "fmt"

// ErrNone is returned when a lookup that requires one match finds nothing.
var ErrNone = fmt.Errorf("no matches")

// ErrMany is returned when a lookup that requires one match finds several.
var ErrMany = fmt.Errorf("more than one match")

// SelectOne returns the only item. Zero items returns ErrNone. More than
// one item returns ErrMany.
func SelectOne[T any](items []T) (T, error) {
	var zero T
	switch len(items) {
	case 0:
		return zero, ErrNone
	case 1:
		return items[0], nil
	default:
		return zero, fmt.Errorf("%w: %d", ErrMany, len(items))
	}
}
