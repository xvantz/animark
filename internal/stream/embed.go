// Package stream provides built-in stream adapters.
package stream

import (
	"context"
	"fmt"
)

// EmbedProvider returns predefined iframe embed URLs.
// This is the canonical example of a StreamProvider — it lives in-repo
// because it contains no third-party content, just URL templating.
//
// The embed provider takes a base URL pattern and returns iframe players
// for any episode. Actual third-party adapters (AniLibria, Crunchyroll,
// etc.) live in separate Go modules and register via stream.Register().
type EmbedProvider struct {
	name    string
	urlTmpl string
	label   string
	lang    string
}

// NewEmbedProvider creates an embed stream provider.
// urlTmpl supports {id} (external ID) and {episode} placeholders.
//
//	stream.NewEmbedProvider("my-player", "https://example.com/embed/{id}/{episode}")
//
// This is safe: no content is distributed, just URL routing.
func NewEmbedProvider(name, urlTmpl, label, lang string) *EmbedProvider {
	return &EmbedProvider{
		name:    name,
		urlTmpl: urlTmpl,
		label:   label,
		lang:    lang,
	}
}

func (e *EmbedProvider) Name() string { return e.name }

// fill replaces {id} and {episode} in the URL template.
func (e *EmbedProvider) fill(providerRef string, episode int) string {
	s := e.urlTmpl
	s = stringsReplace(s, "{provider_ref}", providerRef, 1)
	s = stringsReplace(s, "{id}", providerRef, 1)
	s = stringsReplace(s, "{episode}", fmt.Sprintf("%d", episode), 1)
	return s
}

func (e *EmbedProvider) SearchStreams(ctx context.Context, providerRef string, limit int) ([]*EpisodeSource, error) {
	if limit <= 0 {
		limit = 12
	}
	var sources []*EpisodeSource
	for ep := 1; ep <= limit; ep++ {
		srcID := fmt.Sprintf("%s:%s:ep%d", e.name, providerRef, ep)
		sources = append(sources, &EpisodeSource{
			ID:          srcID,
			Provider:    e.name,
			Episode:     ep,
			PlayType:    "iframe",
			EmbedURL:    e.fill(providerRef, ep),
			Lang:        e.lang,
			Label:       fmt.Sprintf("%s · %s Ep %d", e.label, providerRef, ep),
			ProviderRef: providerRef,
		})
	}
	return sources, nil
}

func (e *EmbedProvider) GetStream(ctx context.Context, id string) (*EpisodeSource, error) {
	// Parse the ID format: "provider:ref:epN"
	var providerName, ref string
	var ep int
	if _, err := fmt.Sscanf(id, "%s:%s:ep%d", &providerName, &ref, &ep); err != nil {
		return nil, fmt.Errorf("invalid embed source id %q", id)
	}
	return &EpisodeSource{
		ID:          id,
		Provider:    e.name,
		Episode:     ep,
		PlayType:    "iframe",
		EmbedURL:    e.fill(ref, ep),
		Lang:        e.lang,
		Label:       fmt.Sprintf("%s · %s Ep %d", e.label, ref, ep),
		ProviderRef: ref,
	}, nil
}

func (e *EmbedProvider) GetPlayerURL(ctx context.Context, source *EpisodeSource) (string, error) {
	if source.EmbedURL != "" {
		return source.EmbedURL, nil
	}
	return "", fmt.Errorf("no embed URL for source %s", source.ID)
}

// stringsReplace is a local wrapper (avoids importing strings for one call).
func stringsReplace(s, old, new string, n int) string {
	if n == 0 {
		return s
	}
	cnt := 0
	for i := 0; i < len(s) && cnt < n; i++ {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			s = s[:i] + new + s[i+len(old):]
			i += len(new) - 1
			cnt++
		}
	}
	return s
}
