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
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/private-service-connect":
		result = map[string]any{"taskId": "get-service"}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-service":
		result = map[string]any{statusKey: "processing-completed", "response": map[string]any{"resource": map[string]any{"id": 7, statusKey: "active"}}}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7":
		result = map[string]any{"taskId": "get-endpoints"}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-endpoints":
		s.childReads++
		endpoints := []any{}
		if s.exists {
			endpoints = append(endpoints, map[string]any{"id": 8, "gcpProjectId": "fixture-project", "gcpVpcName": "fixture-vpc", "gcpVpcSubnetName": "fixture-subnet", "endpointConnectionName": "fixture-prefix", statusKey: "initialized"})
		}
		result = map[string]any{statusKey: "processing-completed", "response": map[string]any{"resource": map[string]any{"pscServiceId": 7, "endpoints": endpoints}}}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7/endpoints/8/creationScripts" && s.exists:
		result = map[string]any{"taskId": "get-script"}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/get-script" && s.exists:
		result = map[string]any{statusKey: "processing-completed", "response": map[string]any{"resource": map[string]any{"script": map[string]any{"terraformGcp": map[string]any{"serviceAttachments": []any{}}}}}}
	case r.Method == http.MethodPost && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7":
		s.creates++
		var body map[string]any
		if s.exists || s.creates != 1 || json.NewDecoder(r.Body).Decode(&body) != nil || body["gcpProjectId"] != "fixture-project" || body["gcpVpcName"] != "fixture-vpc" || body["gcpVpcSubnetName"] != "fixture-subnet" || body["endpointConnectionName"] != "fixture-prefix" {
			s.unknown++
			s.mu.Unlock()
			http.Error(w, "bad create", http.StatusBadRequest)
			return
		}
		s.exists = true
		result = map[string]any{"taskId": "create-endpoint"}
	case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/create-endpoint" && s.exists:
		result = map[string]any{statusKey: "processing-completed", "response": map[string]any{"resourceId": 8}}
	case r.Method == http.MethodDelete && r.URL.Path == "/v1/subscriptions/1/private-service-connect/7/endpoints/8":
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
