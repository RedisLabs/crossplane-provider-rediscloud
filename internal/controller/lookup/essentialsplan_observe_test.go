// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"reflect"
	"testing"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/fixed/plans"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
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
	want := lookupv1alpha1.EssentialsPlanObservation{
		ID:                            "7",
		Name:                          "1GB",
		Size:                          "1",
		SizeMeasurementUnit:           "GB",
		CloudProvider:                 "Azure",
		Region:                        "eastus",
		RegionID:                      "9",
		Availability:                  "No replication",
		Price:                         "10",
		PriceCurrency:                 "USD",
		PricePeriod:                   "Month",
		MaximumDatabases:              "1",
		MaximumThroughput:             "1000",
		MaximumBandwidthGB:            "5",
		Connections:                   "unlimited",
		CidrAllowRules:                "4",
		SupportDataPersistence:        &persistence,
		SupportInstantAndDailyBackups: &backups,
		SupportReplication:            &replication,
		SupportClustering:             &clustering,
		SupportedAlerts:               []string{"dataset-size"},
		CustomerSupport:               "Standard",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}
