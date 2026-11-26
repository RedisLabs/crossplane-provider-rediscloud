package qatfplan

import (
	"context"
	"fmt"
	"os"
	"testing"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// markRawConfigPhase reads current fake-client spec and records presence only.
func markRawConfigPhase(t *testing.T, phase string, kube client.Client, name string) {
	t.Helper()
	path := os.Getenv("QA_RAW_CONFIG_TRACE")
	if path == "" {
		return
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Fatal("raw config trace is hosted only")
	}
	item := &pro.Database{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: name}, item); err != nil {
		t.Fatalf("cannot read fixture spec: %v", err)
	}
	presence := "absent"
	if item.Spec.ForProvider.SourceIps != nil {
		presence = "empty"
		if len(item.Spec.ForProvider.SourceIps) > 0 {
			presence = "nonempty"
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal("cannot open phase trace")
	}
	if _, writeErr := fmt.Fprintf(f, "phase %s %s\n", phase, presence); writeErr != nil {
		_ = f.Close()
		t.Fatal("cannot write phase trace")
	}
	if err = f.Close(); err != nil {
		t.Fatal("cannot close phase trace")
	}
}
