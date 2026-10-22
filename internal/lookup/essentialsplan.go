// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"fmt"
	"strconv"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/fixed/plans"
)

// EssentialsPlanQuery is the filter for one Essentials plan.
type EssentialsPlanQuery struct {
	ID                     string
	Name                   string
	Size                   string
	SizeMeasurementUnit    string
	CloudProvider          string
	Region                 string
	Availability           string
	SupportDataPersistence *bool
	SupportReplication     *bool
}

// MatchEssentialsPlan applies the Terraform rediscloud_essentials_plan
// filters and returns the single match.
func MatchEssentialsPlan(list []*plans.GetPlanResponse, query EssentialsPlanQuery) (*plans.GetPlanResponse, error) {
	var wantID int
	var hasID bool
	if query.ID != "" {
		parsed, err := strconv.Atoi(query.ID)
		if err != nil {
			return nil, fmt.Errorf("plan id %q: %w", query.ID, err)
		}
		wantID = parsed
		hasID = true
	}
	var wantSize float64
	var hasSize bool
	if query.Size != "" {
		parsed, err := strconv.ParseFloat(query.Size, 64)
		if err != nil {
			return nil, fmt.Errorf("plan size %q: %w", query.Size, err)
		}
		wantSize = parsed
		hasSize = true
	}

	matched := make([]*plans.GetPlanResponse, 0, len(list))
	for _, plan := range list {
		if plan == nil {
			continue
		}
		if hasID && redis.IntValue(plan.ID) != wantID {
			continue
		}
		if query.Name != "" && redis.StringValue(plan.Name) != query.Name {
			continue
		}
		if hasSize && redis.Float64Value(plan.Size) != wantSize {
			continue
		}
		if query.SizeMeasurementUnit != "" && redis.StringValue(plan.SizeMeasurementUnit) != query.SizeMeasurementUnit {
			continue
		}
		if query.CloudProvider != "" && redis.StringValue(plan.Provider) != query.CloudProvider {
			continue
		}
		if query.Region != "" && redis.StringValue(plan.Region) != query.Region {
			continue
		}
		if query.Availability != "" && redis.StringValue(plan.Availability) != query.Availability {
			continue
		}
		if query.SupportDataPersistence != nil && redis.BoolValue(plan.SupportDataPersistence) != *query.SupportDataPersistence {
			continue
		}
		if query.SupportReplication != nil && redis.BoolValue(plan.SupportReplication) != *query.SupportReplication {
			continue
		}
		matched = append(matched, plan)
	}
	return SelectOne(matched)
}
