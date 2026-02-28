// Tests for: context_cache.go — TTL cache for service stack types.

package ops

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestStackTypeCache_FreshFetch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		types     []platform.ServiceStackType
		wantCount int
	}{
		{
			name: "returns_fetched_types",
			types: []platform.ServiceStackType{
				{Name: "Node.js", Category: "CORE", Versions: []platform.ServiceStackTypeVersion{
					{Name: "nodejs@22", IsBuild: false, Status: statusActive},
				}},
			},
			wantCount: 1,
		},
		{
			name:      "empty_result",
			types:     nil,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServiceStackTypes(tt.types)
			cache := NewStackTypeCache(time.Hour)

			got := cache.Get(context.Background(), mock)
			if len(got) != tt.wantCount {
				t.Errorf("Get() returned %d types, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestStackTypeCache_CachedWithinTTL(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "CORE"},
	})
	cache := NewStackTypeCache(time.Hour)

	// First call — fetches
	got1 := cache.Get(context.Background(), mock)
	if len(got1) != 1 {
		t.Fatalf("first Get() returned %d types, want 1", len(got1))
	}

	// Change mock data — cached value should be returned
	mock.WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "CORE"},
		{Name: "Go", Category: "CORE"},
	})

	got2 := cache.Get(context.Background(), mock)
	if len(got2) != 1 {
		t.Errorf("second Get() returned %d types, want 1 (cached)", len(got2))
	}
}

func TestStackTypeCache_ExpiredRefresh(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "CORE"},
	})
	// TTL of 0 — always expired
	cache := NewStackTypeCache(0)

	got1 := cache.Get(context.Background(), mock)
	if len(got1) != 1 {
		t.Fatalf("first Get() returned %d types, want 1", len(got1))
	}

	// Update mock — expired cache should re-fetch
	mock.WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "CORE"},
		{Name: "Go", Category: "CORE"},
	})

	got2 := cache.Get(context.Background(), mock)
	if len(got2) != 2 {
		t.Errorf("second Get() returned %d types, want 2 (refreshed)", len(got2))
	}
}

func TestStackTypeCache_APIErrorStaleData(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "CORE"},
	})
	cache := NewStackTypeCache(0) // always expired

	// Prime the cache
	got1 := cache.Get(context.Background(), mock)
	if len(got1) != 1 {
		t.Fatalf("first Get() returned %d types, want 1", len(got1))
	}

	// Inject error — should return stale data
	mock.WithError("ListServiceStackTypes", fmt.Errorf("api down"))

	got2 := cache.Get(context.Background(), mock)
	if len(got2) != 1 {
		t.Errorf("Get() with API error returned %d types, want 1 (stale)", len(got2))
	}
}

func TestStackTypeCache_APIErrorNoCache(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("ListServiceStackTypes", fmt.Errorf("api down"))
	cache := NewStackTypeCache(time.Hour)

	got := cache.Get(context.Background(), mock)
	if got != nil {
		t.Errorf("Get() with API error and no cache returned %v, want nil", got)
	}
}

// slowMock wraps a Mock and adds a delay to ListServiceStackTypes
// to verify that Get() does not hold the mutex during network I/O.
type slowMock struct {
	platform.Client
	delay time.Duration
	types []platform.ServiceStackType
}

func (s *slowMock) ListServiceStackTypes(_ context.Context) ([]platform.ServiceStackType, error) {
	time.Sleep(s.delay)
	return s.types, nil
}

func TestStackTypeCache_NoBlockDuringFetch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		goroutines int
		delay      time.Duration
		maxTotal   time.Duration
	}{
		{
			name:       "5_goroutines_not_serialized",
			goroutines: 5,
			delay:      50 * time.Millisecond,
			maxTotal:   200 * time.Millisecond, // serialized would be 250ms+
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &slowMock{
				Client: platform.NewMock(),
				delay:  tt.delay,
				types: []platform.ServiceStackType{
					{Name: "Node.js", Category: "CORE"},
				},
			}
			cache := NewStackTypeCache(0) // always expired — forces fetch each time

			start := time.Now()
			var wg sync.WaitGroup
			for range tt.goroutines {
				wg.Add(1)
				go func() {
					defer wg.Done()
					got := cache.Get(context.Background(), mock)
					if len(got) != 1 {
						t.Errorf("Get() returned %d types, want 1", len(got))
					}
				}()
			}
			wg.Wait()
			elapsed := time.Since(start)

			if elapsed > tt.maxTotal {
				t.Errorf("concurrent Get() took %v, want < %v (indicates mutex held during I/O)", elapsed, tt.maxTotal)
			}
		})
	}
}

func TestStackTypeCache_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "CORE"},
	})
	cache := NewStackTypeCache(time.Hour)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := cache.Get(context.Background(), mock)
			if len(got) != 1 {
				t.Errorf("concurrent Get() returned %d types, want 1", len(got))
			}
		}()
	}
	wg.Wait()
}
