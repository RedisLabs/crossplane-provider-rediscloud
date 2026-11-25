package qatfplan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/crossplane/upjet/v2/pkg/terraform"
	kexec "k8s.io/utils/exec"
)

const (
	fixtureName     = "fixture"
	dummyCredential = "fake"
	desiredEviction = "allkeys-lru"
)

// This uses the pinned provider and Upjet's real workspace, but no cluster or Cloud account.
func TestEssentialsRefreshAndPlan(t *testing.T) { //nolint:gocyclo // The integration test keeps setup, failure injection and assertions together.
	tf, plugins := os.Getenv("QA_TEST_TERRAFORM"), os.Getenv("QA_TEST_PROVIDER_DIR")
	if tf == "" || plugins == "" {
		t.Skip("set QA_TEST_TERRAFORM and QA_TEST_PROVIDER_DIR for the credential-free integration test")
	}
	var mu sync.Mutex
	eviction, writes, failReads := desiredEviction, 0, false
	badWrites := 0
	databaseUpdates, tagUpdates := 0, 0
	acceptFixtureUpdate := false
	reads, badAuth := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet {
			writes++
			var body map[string]any
			validAuth := r.Header.Get("X-Api-Key") == dummyCredential && r.Header.Get("X-Api-Secret-Key") == dummyCredential
			validBody := json.NewDecoder(r.Body).Decode(&body) == nil
			databaseUpdate := r.Method == http.MethodPut && r.URL.Path == fixtureDatabasePath && validBody && body[fixtureEvictionKey] == desiredEviction
			tagUpdate := r.Method == http.MethodPut && r.URL.Path == fixtureDatabasePath+"/tags" && validBody
			if !validAuth || (!databaseUpdate && (!acceptFixtureUpdate || !tagUpdate)) {
				badWrites++
				http.Error(w, "unexpected fixture write", http.StatusForbidden)
				return
			}
			if databaseUpdate {
				databaseUpdates++
			} else if tagUpdate {
				tagUpdates++
			}
			if !acceptFixtureUpdate {
				http.Error(w, "writes forbidden", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if databaseUpdate {
				eviction = desiredEviction
				_ = json.NewEncoder(w).Encode(map[string]any{fixtureTaskKey: "fixture-task"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{fixtureTagsKey: []any{}})
			return
		}
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			badAuth++
			http.Error(w, "invalid fixture credentials", http.StatusUnauthorized)
			return
		}
		reads++
		if failReads {
			http.Error(w, "read failed", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/tasks/fixture-task":
			_ = json.NewEncoder(w).Encode(map[string]any{fixtureStatusKey: fixtureTaskComplete})
		case fixtureParentPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, fixtureNameKey: fixtureName, fixtureStatusKey: fixtureActive})
		case fixtureDatabasePath + "/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{fixtureTagsKey: []any{}})
		case fixtureDatabasePath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				fixtureDatabaseIDKey: 2, fixtureNameKey: fixtureName, fixtureProtocolKey: fixtureProtocolValue, fixtureProviderKey: fixtureGCP, "region": fixtureRegion, fixtureStatusKey: fixtureActive, fixturePersistenceKey: fixtureNoPersistence, fixtureEvictionKey: eviction, fixtureReplicationKey: false, "activatedOn": "2025-01-01T00:00:00Z", fixtureSecurityKey: map[string]any{"defaultUserEnabled": true, fixtureSourceIPsKey: []string{"0.0.0.0/0"}}, fixtureModulesKey: []any{}, fixtureAlertsKey: []any{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("TF_CLI_CONFIG_FILE", filepath.Join(dir, "empty.tfrc"))
	t.Setenv("TF_CLI_ARGS", "")
	t.Setenv("TF_CLI_ARGS_init", "")
	t.Setenv("TF_CLI_ARGS_import", "")
	t.Setenv("TF_CLI_ARGS_plan", "")
	t.Setenv("TF_CLI_ARGS_apply", "")
	t.Setenv("REDISCLOUD_URL", server.URL+"/v1")
	t.Setenv("REDISCLOUD_ACCESS_KEY", dummyCredential)
	t.Setenv("REDISCLOUD_SECRET_KEY", dummyCredential)
	if writeErr := os.WriteFile(filepath.Join(dir, "empty.tfrc"), nil, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	config := map[string]any{"terraform": map[string]any{"required_providers": map[string]any{fixtureProviderName: map[string]any{"source": fixtureProviderSource, "version": fixtureProviderVersion}}}, fixtureProviderKey: map[string]any{fixtureProviderName: map[string]any{fixtureAPIKey: dummyCredential, fixtureSecretKey: dummyCredential, fixtureURLKey: server.URL + "/v1"}}, "resource": map[string]any{"rediscloud_essentials_database": map[string]any{"test": map[string]any{"subscription_id": 1, fixtureNameKey: fixtureName, "data_eviction": desiredEviction, "data_persistence": fixtureNoPersistence, fixtureReplicationKey: false}}}}
	b, marshalErr := json.Marshal(config)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "main.tf.json"), b, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	run := func(args ...string) {
		t.Helper()
		c := exec.Command(tf, args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "TF_IN_AUTOMATION=1")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("terraform %s failed: %v\n%s", args[0], err, out)
		}
	}
	run("init", "-no-color", "-input=false", "-plugin-dir="+plugins)
	run("import", "-no-color", "rediscloud_essentials_database.test", fixtureExternalID)
	t.Setenv("PATH", fmt.Sprintf("%s%c%s", filepath.Dir(tf), os.PathListSeparator, os.Getenv("PATH")))
	spy := &spyExecutor{Interface: kexec.New()}
	ws := terraform.NewWorkspace(dir, terraform.WithExecutor(spy), terraform.WithFilterFn(func(s string) string { return s }))
	refresh := func() {
		t.Helper()
		result, err := ws.Refresh(context.Background())
		if err != nil || !result.Exists {
			t.Fatalf("refresh: exists=%v err=%v", result.Exists, err)
		}
	}
	plan := func(want bool) {
		t.Helper()
		result, err := ws.Plan(context.Background())
		if err != nil || !result.Exists || result.UpToDate != want {
			t.Fatalf("plan: %#v err=%v", result, err)
		}
	}
	refresh()
	plan(true)
	refresh()
	plan(true)
	callback := &fakeCallback{done: make(chan error, 1)}
	rec, kube := verifyManagedNoop(t, ws, spy, callback)
	verifyManagedIdentity(t, kube)
	mu.Lock()
	noopWrites := writes
	mu.Unlock()
	if noopWrites != 0 {
		t.Fatalf("no-op sent %d fake writes", noopWrites)
	}
	mu.Lock()
	eviction = "noeviction"
	mu.Unlock()
	verifyManagedDrift(t, rec, spy, callback, true)
	mu.Lock()
	driftWrites := writes
	mu.Unlock()
	if driftWrites == 0 {
		t.Fatal("managed drift did not reach the reject-only fake API")
	}
	refresh()
	plan(false)
	mu.Lock()
	failReads = true
	mu.Unlock()
	if _, readErr := ws.Refresh(context.Background()); readErr == nil || !strings.Contains(readErr.Error(), "refresh") {
		t.Fatalf("failed read must fail refresh: %v", readErr)
	}
	mu.Lock()
	failReads = false
	acceptFixtureUpdate = true
	mu.Unlock()
	verifyManagedDrift(t, rec, spy, callback, false)
	mu.Lock()
	if eviction != desiredEviction {
		mu.Unlock()
		t.Fatal("fake database did not converge")
	}
	completedWrites := writes
	mu.Unlock()
	verifyManagedSteady(t, rec, spy)
	verifyManagedIdentity(t, kube)
	mu.Lock()
	if writes != completedWrites {
		mu.Unlock()
		t.Fatal("steady reconciles sent additional fake writes")
	}
	mu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	if writes == 0 || reads == 0 || badAuth != 0 || badWrites != 0 || databaseUpdates != 2 || tagUpdates != 1 {
		t.Fatalf("unexpected fake API traffic: writes=%d reads=%d badAuth=%d badWrites=%d dbUpdates=%d tagUpdates=%d", writes, reads, badAuth, badWrites, databaseUpdates, tagUpdates)
	}
}
