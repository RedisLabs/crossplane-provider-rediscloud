// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"fmt"

	"github.com/RedisLabs/rediscloud-go-api/service/account"
)

// RegionQuery filters the region list.
type RegionQuery struct {
	ProviderName string
	Name         string
}

// MatchRegions returns every region that passes the filter.
// An empty result is ErrNone.
func MatchRegions(regions []*account.Region, query RegionQuery) ([]*account.Region, error) { //nolint:gocyclo // Known mismatches must precede missing-field validation.
	matched := make([]*account.Region, 0, len(regions))
	for _, region := range regions {
		if region == nil {
			return nil, fmt.Errorf("region entry is missing")
		}
		// A known mismatch excludes the row even if another filter field is absent.
		if query.ProviderName != "" && region.Provider != nil && *region.Provider != query.ProviderName {
			continue
		}
		if query.Name != "" && region.Name != nil && *region.Name != query.Name {
			continue
		}
		if region.Provider == nil || region.Name == nil {
			return nil, fmt.Errorf("region observation field is missing")
		}
		if region.ID == nil {
			return nil, fmt.Errorf("region identity is missing")
		}
		matched = append(matched, region)
	}
	if len(matched) == 0 {
		return nil, ErrNone
	}
	return matched, nil
}
