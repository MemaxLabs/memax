package compile

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Which adapters the compile service has (personal-layer design §4). The
// compile contract stays at version 1 when an adapter is added, so the API
// asks the service's /health which kinds it compiles, and refuses to add a
// target of a kind the service doesn't list: a compile of it would fail
// for good. CI deploys the service before the API, so this matters only
// when one is rolled back.

// Healther is a compiler that says which adapters it has (the HTTP
// client; a test fake may not).
type Healther interface {
	Health(ctx context.Context) (*Health, error)
}

// adapterCache remembers the service's adapters between asks.
type adapterCache struct {
	mu    sync.Mutex
	kinds []string
	at    time.Time
}

// How long a list of adapters is trusted, and how soon a kind it lacks is
// asked about again (the service may have been deployed since).
const (
	adaptersFresh = 10 * time.Minute
	adaptersRetry = time.Minute
)

// LoadAdapters asks the service which adapters it has, so the first
// request needn't (the API calls it once at startup).
func (s *Service) LoadAdapters(ctx context.Context) error {
	_, err := s.adapterKinds(ctx, true)
	return err
}

// MissingAdapters lists the kinds the compile service doesn't compile, in
// the order given. It is empty when the service has every one, and when
// Memax can't tell (the service didn't answer, or the compiler can't say):
// a kind is refused only when the service says it doesn't have it.
func (s *Service) MissingAdapters(ctx context.Context, kinds ...ledger.TargetKind) []ledger.TargetKind {
	if s == nil {
		return nil
	}
	have, err := s.adapterKinds(ctx, false)
	if err != nil || have == nil {
		return nil
	}
	missing := absent(have, kinds)
	if len(missing) > 0 && s.cfg.Now().Sub(s.adapterAskedAt()) >= adaptersRetry {
		if have, err = s.adapterKinds(ctx, true); err == nil && have != nil {
			missing = absent(have, kinds)
		}
	}
	return missing
}

func absent(have []string, kinds []ledger.TargetKind) []ledger.TargetKind {
	var out []ledger.TargetKind
	for _, k := range kinds {
		if !slices.Contains(have, string(k)) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func (s *Service) adapterAskedAt() time.Time {
	s.adapters.mu.Lock()
	defer s.adapters.mu.Unlock()
	return s.adapters.at
}

// adapterKinds is the service's adapter kinds, from the cache while fresh
// (or asked again when refresh is set). nil, nil when the compiler can't
// say.
func (s *Service) adapterKinds(ctx context.Context, refresh bool) ([]string, error) {
	h, ok := s.compiler.(Healther)
	if !ok {
		return nil, nil
	}
	s.adapters.mu.Lock()
	defer s.adapters.mu.Unlock()
	now := s.cfg.Now()
	if !refresh && s.adapters.kinds != nil && now.Sub(s.adapters.at) < adaptersFresh {
		return s.adapters.kinds, nil
	}
	health, err := h.Health(ctx)
	s.adapters.at = now
	if err != nil {
		s.cfg.Log.WarnContext(ctx, "compile: the compile service didn't say which adapters it has", "error", err)
		return s.adapters.kinds, err
	}
	kinds := make([]string, 0, len(health.Adapters))
	for _, a := range health.Adapters {
		kinds = append(kinds, a.Kind)
	}
	s.adapters.kinds = kinds
	return kinds, nil
}
