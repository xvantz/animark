// Package provider defines the AnimeProvider interface for metadata sources.
package provider

import (
	"context"

	"github.com/xvantz/animark/internal/core"
)

// AnimeProvider is the interface that all anime data sources must implement.
type AnimeProvider interface {
	// Name returns a unique identifier for this provider (e.g. "shikimori", "anilist").
	Name() string

	// Search returns anime titles matching the query.
	// Each result carries ProviderRefs and Titles but no UUID — the caller assigns one.
	Search(ctx context.Context, query string, limit int) ([]*core.AnimeRecord, error)

	// GetByID returns a single anime by the provider's external ID.
	GetByID(ctx context.Context, id string) (*core.AnimeRecord, error)

	// GetSeasonal returns anime airing in a given season.
	// Season format: "winter", "spring", "summer", "fall"
	GetSeasonal(ctx context.Context, year int, season string) ([]*core.AnimeRecord, error)

	// GetSchedule returns this week's broadcast schedule.
	GetSchedule(ctx context.Context) ([]*core.ScheduleEntry, error)
}

// Registry holds all registered providers.
var registry = make(map[string]AnimeProvider)

// Register adds a provider to the global registry.
func Register(p AnimeProvider) {
	registry[p.Name()] = p
}

// Get returns a registered provider by name.
func Get(name string) AnimeProvider {
	return registry[name]
}

// List returns all registered provider names.
func List() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}
