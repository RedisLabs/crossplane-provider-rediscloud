package qatfplan

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fixtureMutationGate is an in-process demonstration, not an authenticated,
// durable or deployable proxy. The current/stale keys are dummy values.
type fixtureMutationGate struct {
	mu           sync.Mutex
	deleted      map[string]bool
	forwarded    int
	rejectBefore map[string]bool
	preRejected  int
}

func (g *fixtureMutationGate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			http.Error(w, "stale fixture generation", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodDelete {
			if g.rejectBefore[r.URL.Path] {
				delete(g.rejectBefore, r.URL.Path)
				g.preRejected++
				http.Error(w, "fixture pre-dispatch rejection", http.StatusForbidden)
				return
			}
			if g.deleted == nil {
				g.deleted = make(map[string]bool)
			}
			if g.deleted[r.URL.Path] {
				http.Error(w, "duplicate fixture delete", http.StatusConflict)
				return
			}
			g.deleted[r.URL.Path] = true
		}
		g.forwarded++
		next.ServeHTTP(w, r)
	})
}

func (g *fixtureMutationGate) assertRejected(t *testing.T, endpoint, path, key string, expected int) {
	t.Helper()
	g.mu.Lock()
	before := g.forwarded
	g.mu.Unlock()
	req, err := http.NewRequest(http.MethodDelete, endpoint+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("X-Api-Secret-Key", dummyCredential)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	g.mu.Lock()
	after := g.forwarded
	g.mu.Unlock()
	if res.StatusCode != expected || before != after {
		t.Fatalf("fixture gate expected status %d without forwarding; got %d, calls %d to %d", expected, res.StatusCode, before, after)
	}
}

func TestFixtureGatePreDispatchAndUnknownDelete(t *testing.T) {
	requireHostedFixture(t)
	const path = "/fixture/resource"
	gate := &fixtureMutationGate{rejectBefore: map[string]bool{path: true}}
	server := httptest.NewServer(gate.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unknown fake outcome", http.StatusServiceUnavailable)
	})))
	defer server.Close()
	gate.assertRejected(t, server.URL, path, dummyCredential, http.StatusForbidden)
	req, err := http.NewRequest(http.MethodDelete, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", dummyCredential)
	req.Header.Set("X-Api-Secret-Key", dummyCredential)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected forwarded unknown outcome, got %d", res.StatusCode)
	}
	gate.assertRejected(t, server.URL, path, dummyCredential, http.StatusConflict)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.forwarded != 1 {
		t.Fatalf("expected exactly one dispatched delete, got %d", gate.forwarded)
	}
}
