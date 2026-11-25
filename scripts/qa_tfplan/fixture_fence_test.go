package qatfplan

import (
	"net/http"
	"sync"
	"testing"
)

// fixtureMutationGate is an in-process demonstration, not an authenticated,
// durable or deployable proxy. The current/stale keys are dummy values.
type fixtureMutationGate struct {
	mu        sync.Mutex
	deleted   map[string]bool
	forwarded int
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
	defer res.Body.Close()
	g.mu.Lock()
	after := g.forwarded
	g.mu.Unlock()
	if res.StatusCode != expected || before != after {
		t.Fatalf("fixture gate expected status %d without forwarding; got %d, calls %d to %d", expected, res.StatusCode, before, after)
	}
}
