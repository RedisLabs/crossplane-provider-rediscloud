// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/account"
)

// RegionQuery filters the region list.
type RegionQuery struct {
	ProviderName string
	Name         string
}

// MatchRegions returns every region that passes the filter.
// An empty result is ErrNone.
func MatchRegions(regions []*account.Region, query RegionQuery) ([]*account.Region, error) {
	matched := make([]*account.Region, 0, len(regions))
	for _, region := range regions {
		if region == nil {
			continue
		}
		if query.ProviderName != "" && redis.StringValue(region.Provider) != query.ProviderName {
			continue
		}
		if query.Name != "" && redis.StringValue(region.Name) != query.Name {
			continue
		}
		matched = append(matched, region)
	}
	if len(matched) == 0 {
		return nil, ErrNone
	}
	return matched, nil
}
