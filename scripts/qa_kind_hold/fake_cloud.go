// Disposable fake-only HTTP API. It never forwards requests to Cloud.
package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
)

const statusKey = "status"

type state struct {
	mu                                    sync.Mutex
	exists                                bool
	deleteSource                          string
	creates, deletes, childReads, unknown int
	release                               chan struct{}
	once                                  sync.Once
}

func (s *state) serve(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // The fixture rejects every unexpected route explicitly.
	if r.URL.Path == "/fixture/state" && r.Method == http.MethodGet && r.Header.Get("X-Fixture-Control") == "fixture-control" {
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"exists": s.exists, "creates": s.creates, "deletes": s.deletes, "reads": s.childReads, "unknown": s.unknown, "deleteSource": s.deleteSource})
		return
	}
	if r.URL.Path == "/fixture/release" && r.Method == http.MethodPost && r.Header.Get("X-Fixture-Control") == "fixture-control" {
		s.once.Do(func() { close(s.release) })
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Header.Get("X-Api-Key") != "fixture-key" || r.Header.Get("X-Api-Secret-Key") != "fixture-secret" {
		s.mu.Lock()
		s.unknown++
		s.mu.Unlock()
		http.Error(w, "denied", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	w.Header().Set("Content-Type", "application/json")
	var result any
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1":
		result = map[string]any{"id": 1, "name": "fake-parent", statusKey: "active"}
	case r.Method == http.MethodPost && r.URL.Path == "/v1/subscriptions/1/databases":
		s.creates++
		var body map[string]any
		if s.exists || s.creates != 1 || json.NewDecoder(r.Body).Decode(&body) != nil || body["name"] != "fake-database" {
			s.unknown++
			s.mu.Unlock()
			http.Error(w, "bad create", http.StatusBadRequest)
			return
		}
		s.exists = true
		result = map[string]any{"taskId": "create"}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/create":
		result = map[string]any{statusKey: "processing-completed", "response": map[string]any{"resourceId": 42}}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/databases/42" && s.exists:
		s.childReads++
		result = map[string]any{"databaseId": 42, "name": "fake-database", "dataEvictionPolicy": "allkeys-lru", statusKey: "active", "protocol": "redis", "dataPersistence": "none", "replication": false, "memoryLimitInGb": 1, "throughputMeasurement": map[string]any{"by": "operations-per-second", "value": 1000}, "security": map[string]any{"enableDefaultUser": true, "enableTls": false, "tlsClientAuthentication": false, "sourceIps": []any{}}, "modules": []any{}, "alerts": []any{}, "clustering": map[string]any{"regexRules": []any{}}}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/databases/42/tags":
		result = map[string]any{"tags": []any{}}
	case r.Method == http.MethodPut && r.URL.Path == "/v1/subscriptions/1/databases/42" && s.exists:
		result = map[string]any{"taskId": "update"}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/update":
		result = map[string]any{statusKey: "processing-completed"}
	case r.Method == http.MethodPut && r.URL.Path == "/v1/subscriptions/1/databases/42/tags":
		result = map[string]any{"tags": []any{}}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/databases":
		rows := []any{}
		if s.exists {
			rows = append(rows, map[string]any{"databaseId": 42, "name": "fake-database", statusKey: "active"})
		}
		result = map[string]any{"databases": rows}
	case r.Method == http.MethodDelete && r.URL.Path == "/v1/subscriptions/1/databases/42":
		s.deletes++
		source, _, sourceErr := net.SplitHostPort(r.RemoteAddr)
		if sourceErr != nil {
			s.unknown++
		} else {
			s.deleteSource = source
		}
		if s.deletes != 1 || !s.exists {
			s.unknown++
		}
		s.mu.Unlock()
		<-s.release
		http.Error(w, "accepted; response deliberately withheld", http.StatusServiceUnavailable)
		return
	default:
		s.unknown++
		s.mu.Unlock()
		http.Error(w, "unknown fake route", http.StatusForbidden)
		return
	}
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(result)
}
func main() {
	s := &state{release: make(chan struct{})}
	log.Fatal(http.ListenAndServe(":5556", http.HandlerFunc(s.serve)))
}
