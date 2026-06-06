// Package stream provides built-in stream adapters.
package stream

import (
	"context"
	"fmt"
)

// CrunchyrollProvider is a skeleton adapter for Crunchyroll's official API.
//
// ⚠️ This is a structural example, NOT a working implementation.
// A real Crunchyroll adapter requires:
//   - Crunchyroll API credentials (client ID, secret) obtained from Crunchyroll
//   - OAuth device flow authentication
//   - An active Crunchyroll subscription
//   - HLS license token acquisition (DRM)
//
// The actual implementation must live in a separate Go module:
//
//	import _ "github.com/yourname/animark-crunchyroll"
//
// and call stream.Register() in its init().
type CrunchyrollProvider struct {
	// In a real implementation, these would be populated via OAuth flow.
	accessToken string
	// Subscriber status — checked at runtime.
	isSubscribed bool
}

// NewCrunchyrollProvider creates a Crunchyroll stream provider stub.
func NewCrunchyrollProvider() *CrunchyrollProvider {
	return &CrunchyrollProvider{}
}

func (c *CrunchyrollProvider) Name() string { return "crunchyroll" }

func (c *CrunchyrollProvider) SearchStreams(ctx context.Context, providerRef string, limit int) ([]*EpisodeSource, error) {
	return nil, fmt.Errorf("crunchyroll adapter: implement in separate module (see stream/crunchyroll.go docs)")
}

func (c *CrunchyrollProvider) GetStream(ctx context.Context, id string) (*EpisodeSource, error) {
	return nil, fmt.Errorf("crunchyroll adapter: implement in separate module")
}

func (c *CrunchyrollProvider) GetPlayerURL(ctx context.Context, source *EpisodeSource) (string, error) {
	return "", fmt.Errorf("crunchyroll adapter: implement in separate module")
}
