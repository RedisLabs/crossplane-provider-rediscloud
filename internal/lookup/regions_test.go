// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"errors"
	"testing"

	"github.com/RedisLabs/rediscloud-go-api/service/account"
)

func TestMatchRegions(t *testing.T) {
	regions := []*account.Region{
		region(1, "us-east-1", "AWS"),
		region(2, "us-central1", "GCP"),
		nil,
	}
	got, err := MatchRegions(regions, RegionQuery{ProviderName: "GCP", Name: "us-central1"})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(got) != 1 || *got[0].ID != 2 {
		t.Fatalf("got %#v", got)
	}
	_, err = MatchRegions(regions, RegionQuery{ProviderName: "Azure"})
	if !errors.Is(err, ErrNone) {
		t.Fatalf("none: %v", err)
	}
}

func region(id int, name, provider string) *account.Region {
	return &account.Region{ID: &id, Name: &name, Provider: &provider}
}
