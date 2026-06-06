// Package provider includes adapters and wrappers for anime data sources.
package provider

import (
	"context"

	"github.com/xvantz/animark/internal/core"
)

// EnglishAdapter wraps an AnimeProvider and ensures all results have English
// titles populated. It delegates the actual API calls and post-processes the
// responses to guarantee English-language metadata.
//
// Use case: register EnglishAdapter(anilist.NewClient()) as "english" to get
// a provider that always returns English titles regardless of the source.
type EnglishAdapter struct {
	delegate AnimeProvider
}

// NewEnglishAdapter creates a provider wrapper that normalizes titles to English.
func NewEnglishAdapter(delegate AnimeProvider) *EnglishAdapter {
	return &EnglishAdapter{delegate: delegate}
}

func (e *EnglishAdapter) Name() string {
	return e.delegate.Name() + "-en"
}

func (e *EnglishAdapter) Search(ctx context.Context, query string, limit int) ([]*core.AnimeRecord, error) {
	results, err := e.delegate.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		e.ensureEnglish(r)
	}
	return results, nil
}

func (e *EnglishAdapter) GetByID(ctx context.Context, id string) (*core.AnimeRecord, error) {
	result, err := e.delegate.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	e.ensureEnglish(result)
	return result, nil
}

func (e *EnglishAdapter) GetSeasonal(ctx context.Context, year int, season string) ([]*core.AnimeRecord, error) {
	results, err := e.delegate.GetSeasonal(ctx, year, season)
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		e.ensureEnglish(r)
	}
	return results, nil
}

func (e *EnglishAdapter) GetSchedule(ctx context.Context) ([]*core.ScheduleEntry, error) {
	return e.delegate.GetSchedule(ctx)
}

// ensureEnglish normalizes titles so English is always populated.
func (e *EnglishAdapter) ensureEnglish(r *core.AnimeRecord) {
	if r.Titles == nil {
		r.Titles = make(map[string]string)
	}

	// If no English title, fall back to Romaji (ja) or native.
	if r.Titles["en"] == "" {
		if r.Titles["ja"] != "" {
			r.Titles["en"] = r.Titles["ja"]
		} else if r.Titles["native"] != "" {
			r.Titles["en"] = r.Titles["native"]
		}
	}

	// Clear Russian titles — English provider doesn't need them.
	delete(r.Titles, "ru")
}
