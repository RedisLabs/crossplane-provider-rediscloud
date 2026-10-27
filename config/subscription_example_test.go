package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	rediscloudv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/rediscloud/v1alpha1"
)

// subscriptionExampleBeforeFix is the manifest Redis Cloud rejected on
// 27 October 2026: no payment method id, no dataset size, and
// number-of-shards without RediSearch.
const subscriptionExampleBeforeFix = `
apiVersion: rediscloud.redis.io/v1alpha1
kind: Subscription
metadata:
  name: example-subscription
spec:
  forProvider:
    name: "my-redis-subscription"
    memoryStorage: "ram"
    paymentMethod: "credit-card"
    cloudProvider:
    - provider: "AWS"
      region:
      - region: "us-east-1"
        networkingDeploymentCidr: "10.0.0.0/24"
        multipleAvailabilityZones: true
    creationPlan:
    - quantity: 1
      replication: true
      throughputMeasurementBy: "number-of-shards"
      throughputMeasurementValue: 1
  providerConfigRef:
    name: default
`

func TestSubscriptionExampleParameters(t *testing.T) {
	t.Parallel()
	problems := subscriptionExampleProblems(t, subscriptionExampleYAML(t))
	if len(problems) > 0 {
		t.Fatalf("examples/rediscloud/subscription.yaml: %s", strings.Join(problems, "; "))
	}
}

func TestSubscriptionExampleRejectsTheShapeRedisRejected(t *testing.T) {
	t.Parallel()
	problems := subscriptionExampleProblems(t, []byte(subscriptionExampleBeforeFix))
	for _, want := range []string{"payment_method_id", "dataset_size_in_gb", "operations-per-second"} {
		if !strings.Contains(strings.Join(problems, "; "), want) {
			t.Fatalf("problems %q do not mention %s", problems, want)
		}
	}
}

func TestSubscriptionExampleProblemsWhenFieldsAreRemoved(t *testing.T) {
	t.Parallel()
	params := mustSubscriptionParameters(t, subscriptionExampleYAML(t))
	if problems := subscriptionParameterProblems(params); len(problems) > 0 {
		t.Fatalf("checked-in example is already invalid: %s", strings.Join(problems, "; "))
	}

	withoutID := cloneParameters(params)
	delete(withoutID, "payment_method_id")
	if problems := subscriptionParameterProblems(withoutID); !containsAll(problems, "payment_method_id") {
		t.Fatalf("removed payment_method_id: %q", problems)
	}

	withoutSize := cloneParameters(params)
	plan := creationPlan(t, withoutSize)
	delete(plan, "dataset_size_in_gb")
	delete(plan, "memory_limit_in_gb")
	if problems := subscriptionParameterProblems(withoutSize); !containsAll(problems, "dataset_size_in_gb", "memory_limit_in_gb") {
		t.Fatalf("removed size: %q", problems)
	}

	shards := cloneParameters(params)
	creationPlan(t, shards)["throughput_measurement_by"] = "number-of-shards"
	if problems := subscriptionParameterProblems(shards); !containsAll(problems, "operations-per-second") {
		t.Fatalf("number-of-shards: %q", problems)
	}
}

func subscriptionExampleProblems(t *testing.T, raw []byte) []string {
	t.Helper()
	return subscriptionParameterProblems(mustSubscriptionParameters(t, raw))
}

func mustSubscriptionParameters(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var sub rediscloudv1alpha1.Subscription
	if err := yaml.Unmarshal(raw, &sub); err != nil {
		t.Fatalf("unmarshal subscription: %v", err)
	}
	params, err := sub.GetParameters()
	if err != nil {
		t.Fatalf("GetParameters: %v", err)
	}
	return params
}

func subscriptionParameterProblems(params map[string]any) []string {
	var problems []string
	if strings.TrimSpace(fmt.Sprint(params["payment_method_id"])) == "" || params["payment_method_id"] == nil {
		problems = append(problems, "missing payment_method_id")
	}
	plans, ok := params["creation_plan"].([]any)
	if !ok || len(plans) == 0 {
		return append(problems, "missing creation_plan size and throughput_measurement_by operations-per-second")
	}
	plan, ok := plans[0].(map[string]any)
	if !ok {
		return append(problems, "creation_plan[0] is not an object")
	}
	if plan["dataset_size_in_gb"] == nil && plan["memory_limit_in_gb"] == nil {
		problems = append(problems, "missing dataset_size_in_gb or memory_limit_in_gb")
	}
	if plan["throughput_measurement_by"] != "operations-per-second" {
		problems = append(problems, fmt.Sprintf("throughput_measurement_by = %v, want operations-per-second", plan["throughput_measurement_by"]))
	}
	return problems
}

func creationPlan(t *testing.T, params map[string]any) map[string]any {
	t.Helper()
	plans, ok := params["creation_plan"].([]any)
	if !ok || len(plans) == 0 {
		t.Fatal("creation_plan is empty")
	}
	plan, ok := plans[0].(map[string]any)
	if !ok {
		t.Fatal("creation_plan[0] is not an object")
	}
	return plan
}

func cloneParameters(params map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range params {
		plans, ok := value.([]any)
		if !ok {
			out[key] = value
			continue
		}
		copied := make([]any, len(plans))
		for i, item := range plans {
			plan, ok := item.(map[string]any)
			if !ok {
				copied[i] = item
				continue
			}
			planCopy := map[string]any{}
			for planKey, planValue := range plan {
				planCopy[planKey] = planValue
			}
			copied[i] = planCopy
		}
		out[key] = copied
	}
	return out
}

func containsAll(problems []string, parts ...string) bool {
	joined := strings.Join(problems, "; ")
	for _, part := range parts {
		if !strings.Contains(joined, part) {
			return false
		}
	}
	return true
}

func subscriptionExampleYAML(t *testing.T) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "examples", "rediscloud", "subscription.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
