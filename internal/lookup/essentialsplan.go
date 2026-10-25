// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/fixed/plans"
)

// CanonicalProvider maps AWS, GCP, and Azure to the values the Redis Cloud
// API returns. Other providers are returned trimmed, so a typo stays visible.
func CanonicalProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "aws":
		return "AWS"
	case "gcp":
		return "GCP"
	case "azure":
		return "Azure"
	default:
		return strings.TrimSpace(value)
	}
}

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
	filters, err := planFilters(query)
	if err != nil {
		return nil, err
	}
	matched := make([]*plans.GetPlanResponse, 0, len(list))
	for _, plan := range list {
		if plan == nil || !planMatches(plan, filters) {
			continue
		}
		matched = append(matched, plan)
	}
	return SelectOne(matched)
}

func planFilters(query EssentialsPlanQuery) ([]func(*plans.GetPlanResponse) bool, error) {
	filters := make([]func(*plans.GetPlanResponse) bool, 0, 9)
	if query.ID != "" {
		parsed, err := strconv.Atoi(query.ID)
		if err != nil {
			return nil, fmt.Errorf("plan id %q: %w", query.ID, err)
		}
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.IntValue(plan.ID) == parsed
		})
	}
	if query.Size != "" {
		parsed, err := strconv.ParseFloat(query.Size, 64)
		if err != nil {
			return nil, fmt.Errorf("plan size %q: %w", query.Size, err)
		}
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.Float64Value(plan.Size) == parsed
		})
	}
	filters = append(filters, textFilters(query)...)
	filters = append(filters, boolFilters(query)...)
	return filters, nil
}

func textFilters(query EssentialsPlanQuery) []func(*plans.GetPlanResponse) bool {
	filters := make([]func(*plans.GetPlanResponse) bool, 0, 5)
	if query.Name != "" {
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.StringValue(plan.Name) == query.Name
		})
	}
	if query.SizeMeasurementUnit != "" {
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.StringValue(plan.SizeMeasurementUnit) == query.SizeMeasurementUnit
		})
	}
	if query.CloudProvider != "" {
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return strings.EqualFold(redis.StringValue(plan.Provider), query.CloudProvider)
		})
	}
	if query.Region != "" {
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.StringValue(plan.Region) == query.Region
		})
	}
	if query.Availability != "" {
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.StringValue(plan.Availability) == query.Availability
		})
	}
	return filters
}

func boolFilters(query EssentialsPlanQuery) []func(*plans.GetPlanResponse) bool {
	filters := make([]func(*plans.GetPlanResponse) bool, 0, 2)
	if query.SupportDataPersistence != nil {
		want := *query.SupportDataPersistence
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.BoolValue(plan.SupportDataPersistence) == want
		})
	}
	if query.SupportReplication != nil {
		want := *query.SupportReplication
		filters = append(filters, func(plan *plans.GetPlanResponse) bool {
			return redis.BoolValue(plan.SupportReplication) == want
		})
	}
	return filters
}

func planMatches(plan *plans.GetPlanResponse, filters []func(*plans.GetPlanResponse) bool) bool {
	for _, filter := range filters {
		if !filter(plan) {
			return false
		}
	}
	return true
}
