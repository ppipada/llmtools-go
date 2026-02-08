package executil

import (
	"testing"
	"time"
)

func TestSessionStore_TTL_EvictsWithoutSleep(t *testing.T) {
	cases := []struct {
		name      string
		ttl       time.Duration
		age       time.Duration
		wantEvict bool
	}{
		{name: "ttl_disabled_never_evicts", ttl: 0, age: 24 * time.Hour, wantEvict: false},
		{name: "not_old_enough", ttl: 10 * time.Second, age: 1 * time.Second, wantEvict: false},
		{name: "old_enough", ttl: 100 * time.Millisecond, age: 2 * time.Second, wantEvict: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ss := NewSessionStore()
			ss.SetTTL(tc.ttl)

			s := ss.NewSession()
			if s == nil || s.id == "" {
				t.Fatalf("expected session")
			}

			// Force lastUsed to the past deterministically.
			ss.mu.Lock()
			e := ss.m[s.id]
			if e == nil {
				ss.mu.Unlock()
				t.Fatalf("missing store entry")
			}
			it, _ := e.Value.(*sessionItem)
			if it == nil {
				ss.mu.Unlock()
				t.Fatalf("missing sessionItem")
			}
			it.lastUsed = time.Now().Add(-tc.age)
			ss.mu.Unlock()

			_, ok := ss.Get(s.id) // get() performs eviction check
			if tc.wantEvict && ok {
				t.Fatalf("expected evicted, but get() returned ok")
			}
			if !tc.wantEvict && !ok {
				t.Fatalf("expected present, but get() returned !ok")
			}

			s.mu.RLock()
			closed := s.closed
			s.mu.RUnlock()
			if tc.wantEvict && !closed {
				t.Fatalf("expected closed session after eviction")
			}
		})
	}
}
