// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"testing"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/fixed/plans"
)

func TestObservePlanCopiesCapabilityFields(t *testing.T) {
	persistence := false
	backups := true
	replication := false
	clustering := true
	alert := "dataset-size"
	got := observePlan(&plans.GetPlanResponse{
		ID:                            redis.Int(7),
		Name:                          redis.String("1GB"),
		Size:                          redis.Float64(1),
		SizeMeasurementUnit:           redis.String("GB"),
		Provider:                      redis.String("Azure"),
		Region:                        redis.String("eastus"),
		RegionID:                      redis.Int(9),
		Availability:                  redis.String("No replication"),
		Price:                         redis.Int(10),
		PriceCurrency:                 redis.String("USD"),
		PricePeriod:                   redis.String("Month"),
		MaximumDatabases:              redis.Int(1),
		MaximumThroughput:             redis.Int(1000),
		MaximumBandwidthGB:            redis.Int(5),
		Connections:                   redis.String("unlimited"),
		CidrAllowRules:                redis.Int(4),
		SupportDataPersistence:        &persistence,
		SupportInstantAndDailyBackups: &backups,
		SupportReplication:            &replication,
		SupportClustering:             &clustering,
		SupportedAlerts:               []*string{&alert, nil},
		CustomerSupport:               redis.String("Standard"),
	})
	if got.CloudProvider != "Azure" || got.MaximumDatabases != "1" || got.MaximumThroughput != "1000" || got.MaximumBandwidthGB != "5" {
		t.Fatalf("identity and limits: %#v", got)
	}
	if got.Connections != "unlimited" || got.CidrAllowRules != "4" || got.CustomerSupport != "Standard" {
		t.Fatalf("connections: %#v", got)
	}
	if got.SupportDataPersistence == nil || *got.SupportDataPersistence {
		t.Fatal("data persistence should be false")
	}
	if got.SupportReplication == nil || *got.SupportReplication {
		t.Fatal("replication should be false")
	}
	if got.SupportInstantAndDailyBackups == nil || !*got.SupportInstantAndDailyBackups {
		t.Fatal("backups should be true")
	}
	if got.SupportClustering == nil || !*got.SupportClustering {
		t.Fatal("clustering should be true")
	}
	if len(got.SupportedAlerts) != 1 || got.SupportedAlerts[0] != "dataset-size" {
		t.Fatalf("alerts: %#v", got.SupportedAlerts)
	}
}
