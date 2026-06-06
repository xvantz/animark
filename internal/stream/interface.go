// Package stream defines the StreamProvider interface for video playback
// sources. Each provider (Crunchyroll, AniLibria, embed iframe, etc.)
// adapts its video delivery to this shape.
//
// The interface lives in the core repository; actual implementations are
// separate Go modules that third parties maintain independently.
package stream

import (
	"context"
	"fmt"
	"time"
)

// EpisodeSource describes a playable video source for a specific episode.
type EpisodeSource struct {
	// ID is a unique identifier for this source within the provider.
	ID string `json:"id"`
	// Provider name (e.g. "crunchyroll", "anilibria", "embed").
	Provider string `json:"provider"`
	// Episode number.
	Episode int `json:"episode"`
	// PlayType describes how the video is delivered.
	PlayType string `json:"play_type"` // "iframe", "hls", "mp4", "dash"
	// EmbedURL is a URL to embed in an iframe (for iframe type).
	EmbedURL string `json:"embed_url,omitempty"`
	// StreamURL is a direct media URL (for hls/mp4/dash types).
	StreamURL string `json:"stream_url,omitempty"`
	// Language tag: "ru", "en", "sub_ru", "sub_en", "ja", etc.
	Lang string `json:"lang,omitempty"`
	// Quality label: "1080p", "720p", etc.
	Quality string `json:"quality,omitempty"`
	// Label for the source (e.g. "Crunchyroll · Sub", "AniLibria · Озвучка").
	Label string `json:"label,omitempty"`
	// ExpiresAt is when the stream URL expires (if applicable).
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// AnimeTitle for display purposes.
	AnimeTitle string `json:"anime_title,omitempty"`
	// ProviderRef is the provider:external_id this source was resolved from.
	ProviderRef string `json:"provider_ref,omitempty"`
}

// StreamProvider is the interface that all video source adapters implement.
// Each provider maps an external identifier to playable episode sources.
type StreamProvider interface {
	// Name returns a short unique identifier (e.g. "crunchyroll", "embed").
	Name() string

	// SearchStreams finds available video sources for a given anime.
	// The providerRef is a "provider:external_id" from the core library
	// (e.g. "shikimori:59970"). The provider uses it to look up its own
	// internal identifier.
	// Returns all available episodes with their sources.
	SearchStreams(ctx context.Context, providerRef string, limit int) ([]*EpisodeSource, error)

	// GetStream returns a specific episode's source.
	GetStream(ctx context.Context, episodeSourceID string) (*EpisodeSource, error)

	// GetPlayerURL returns an embeddable player URL for a source.
	// For iframe types, this is the embed URL; for HLS/MP4 this is the
	// media URL. Some providers may add authentication tokens at this stage.
	GetPlayerURL(ctx context.Context, source *EpisodeSource) (string, error)
}

// Registry holds all registered stream providers.
var registry = make(map[string]StreamProvider)

// Register adds a stream provider to the global registry.
func Register(p StreamProvider) {
	registry[p.Name()] = p
}

// Get returns a registered stream provider by name.
func Get(name string) StreamProvider {
	return registry[name]
}

// List returns all registered stream provider names.
func List() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}

// ErrNoStreams is returned when a provider has no sources for an anime.
var ErrNoStreams = fmt.Errorf("no streams available")
