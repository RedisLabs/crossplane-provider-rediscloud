// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"errors"
	"testing"

	"github.com/RedisLabs/rediscloud-go-api/service/fixed/plans"
)

func TestMatchEssentialsPlan(t *testing.T) {
	no := false
	plansList := []*plans.GetPlanResponse{
		plan(1, "small", 1, "GB", "GCP", "us-central1", "Single-zone", false),
		plan(2, "small", 1, "GB", "AWS", "us-east-1", "Multi-zone", true),
	}
	got, err := MatchEssentialsPlan(plansList, EssentialsPlanQuery{
		CloudProvider:      "GCP",
		Region:             "us-central1",
		Size:               "1",
		SupportReplication: &no,
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if redisPlanID(got) != 1 {
		t.Fatalf("id = %d", redisPlanID(got))
	}

	_, err = MatchEssentialsPlan(plansList, EssentialsPlanQuery{Name: "small"})
	if !errors.Is(err, ErrMany) {
		t.Fatalf("many: %v", err)
	}
	_, err = MatchEssentialsPlan(plansList, EssentialsPlanQuery{ID: "9"})
	if !errors.Is(err, ErrNone) {
		t.Fatalf("none: %v", err)
	}
	_, err = MatchEssentialsPlan(plansList, EssentialsPlanQuery{Size: "nope"})
	if err == nil {
		t.Fatal("expected size parse error")
	}
}

func plan(id int, name string, size float64, unit, provider, region, availability string, replication bool) *plans.GetPlanResponse {
	return &plans.GetPlanResponse{
		ID:                  &id,
		Name:                &name,
		Size:                &size,
		SizeMeasurementUnit: &unit,
		Provider:            &provider,
		Region:              &region,
		Availability:        &availability,
		SupportReplication:  &replication,
	}
}

func redisPlanID(plan *plans.GetPlanResponse) int {
	if plan == nil || plan.ID == nil {
		return 0
	}
	return *plan.ID
}
