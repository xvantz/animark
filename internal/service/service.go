// Package service ties together providers, storage, and git sync.
package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xvantz/animark/internal/cache"
	"github.com/xvantz/animark/internal/core"
	"github.com/xvantz/animark/internal/history"
	"github.com/xvantz/animark/internal/provider"
	"github.com/xvantz/animark/internal/rate"
)

// Service is the main application API used by HTTP handlers and CLI commands.
type Service struct {
	store     core.Store
	gitSync   *core.GitSync
	providers map[string]provider.AnimeProvider
	provCache *cache.Cache
	limiter   *rate.Limiter
	history   *history.Logger
}

func New(store core.Store, gitSync *core.GitSync) *Service {
	providers := make(map[string]provider.AnimeProvider)
	for _, name := range provider.List() {
		providers[name] = provider.Get(name)
	}
	return &Service{
		store:     store,
		gitSync:   gitSync,
		providers: providers,
		provCache: cache.New(30*time.Minute, 5*time.Minute),
		limiter:   rate.New(30, 1*time.Second),
	}
}

// WithHistory attaches a history logger. If set, the service logs all
// status changes and episode updates to the history file.
func (s *Service) WithHistory(hl *history.Logger) *Service {
	s.history = hl
	return s
}

// ---- ID resolution ----

// ResolveID resolves a user-facing ID (UUID or "provider:id") to a UUID.
// It also returns the resolved AnimeRecord and whether the input was a composite ID.
func (s *Service) ResolveID(ctx context.Context, id string) (uuid string, err error) {
	// If it contains a colon, treat as provider:external_id
	if providerName, externalID, ok := strings.Cut(id, ":"); ok && providerName != "" && externalID != "" {
		db, err := s.store.Load(ctx)
		if err != nil {
			return "", err
		}
		for _, a := range db.Anime {
			for _, ref := range a.ProviderRefs {
				if ref.Provider == providerName && ref.ExternalID == externalID {
					return a.ID, nil
				}
			}
		}
		// Not found in DB — could be a new lookup. Return the composite form
		// so callers can fetch from provider and create a new record.
		return "", fmt.Errorf("anime %s not found in local database; use 'add' first", id)
	}
	// Assume it's a UUID
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		return id, nil
	}
	return "", fmt.Errorf("invalid id %q: expected UUID or provider:id", id)
}

// LookupByProviderRef scans the database for an anime matching the given provider ref.
// Returns nil if not found.
func lookupByProviderRef(db *core.UserDatabase, providerName, externalID string) *core.AnimeRecord {
	for i := range db.Anime {
		for _, ref := range db.Anime[i].ProviderRefs {
			if ref.Provider == providerName && ref.ExternalID == externalID {
				return &db.Anime[i]
			}
		}
	}
	return nil
}

// ---- Rate-limited provider calls ----

func (s *Service) callProvider(ctx context.Context, name string, fn func(p provider.AnimeProvider) error) error {
	p := provider.Get(name)
	if p == nil {
		return fmt.Errorf("unknown provider: %s", name)
	}
	if !s.limiter.Allow(name) {
		return fmt.Errorf("rate limit exceeded for provider %s, try again later", name)
	}
	return fn(p)
}

// ---- Anime metadata (from providers with cache) ----

// SearchAnime searches across all providers, deduplicating results by
// normalized English title. When the same anime appears from multiple
// providers, their metadata and provider refs are merged.
func (s *Service) SearchAnime(ctx context.Context, query string, limit int) ([]*core.AnimeRecord, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var all []*core.AnimeRecord
	for _, p := range s.providers {
		if !s.limiter.Allow(p.Name()) {
			continue
		}
		results, err := p.Search(ctx, query, limit)
		if err != nil {
			log.Printf("provider %s search error: %v", p.Name(), err)
			continue
		}
		all = append(all, results...)
	}
	return deduplicateAnime(all), nil
}

// deduplicateAnime merges AnimeRecords that refer to the same title.
// Uses normalized English title as the match key.
func deduplicateAnime(results []*core.AnimeRecord) []*core.AnimeRecord {
	seen := make(map[string]*core.AnimeRecord)
	order := make([]string, 0)

	for _, r := range results {
		key := normalizeTitle(r.Title("en"))
		if key == "" {
			key = normalizeTitle(r.Title("ja"))
		}
		if key == "" {
			continue
		}

		if existing, ok := seen[key]; ok {
			existing.MergeFrom(r)
		} else {
			seen[key] = r
			order = append(order, key)
		}
	}

	deduped := make([]*core.AnimeRecord, 0, len(order))
	for _, key := range order {
		deduped = append(deduped, seen[key])
	}
	return deduped
}

// normalizeTitle prepares a title for fuzzy comparison.
func normalizeTitle(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return ""
	}
	// Remove common suffixes that differ between providers
	suffixes := []string{" season", " 2nd season", " 3rd season", " 4th season",
		" 2", " ii", " iii", " iv",
		" (tv)", " (ona)", " (ova)", " (movie)",
		" part 1", " part 2", " part 3",
		" 1st season", " 2nd cour", " cour 1", " cour 2", " cour 3"}
	for _, suf := range suffixes {
		if strings.HasSuffix(title, suf) {
			title = title[:len(title)-len(suf)]
		}
	}
	return title
}

// GetAnime fetches a single anime by UUID or "provider:id".
// First checks local DB, then falls back to provider API.
func (s *Service) GetAnime(ctx context.Context, id string) (*core.AnimeRecord, error) {
	// Check local DB first.
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}

	// Try UUID lookup.
	for _, a := range db.Anime {
		if a.ID == id {
			return &a, nil
		}
	}

	// Try provider:id lookup.
	if providerName, externalID, ok := strings.Cut(id, ":"); ok {
		// Check DB by provider ref.
		if a := lookupByProviderRef(db, providerName, externalID); a != nil {
			return a, nil
		}
		// Fall back to provider API.
		cacheKey := "anime:" + id
		if cached, ok := s.provCache.Get(cacheKey); ok {
			return cached.(*core.AnimeRecord), nil
		}

		p := provider.Get(providerName)
		if p == nil {
			return nil, fmt.Errorf("unknown provider: %s", providerName)
		}
		a, err := p.GetByID(ctx, externalID)
		if err != nil {
			return nil, err
		}
		s.provCache.Set(cacheKey, a)
		return a, nil
	}

	return nil, fmt.Errorf("anime not found: %s", id)
}

// GetSeasonal fetches seasonal anime from the first available provider.
func (s *Service) GetSeasonal(ctx context.Context, year int, season string) ([]*core.AnimeRecord, error) {
	cacheKey := fmt.Sprintf("seasonal:%d-%s", year, season)
	if cached, ok := s.provCache.Get(cacheKey); ok {
		return cached.([]*core.AnimeRecord), nil
	}

	for _, p := range s.providers {
		results, err := p.GetSeasonal(ctx, year, season)
		if err != nil {
			log.Printf("provider %s seasonal error: %v", p.Name(), err)
			continue
		}
		s.provCache.Set(cacheKey, results)
		return results, nil
	}
	return nil, fmt.Errorf("no provider available for seasonal data")
}

// GetSchedule returns this week's schedule from the first available provider.
func (s *Service) GetSchedule(ctx context.Context) ([]*core.ScheduleEntry, error) {
	cacheKey := "schedule"
	if cached, ok := s.provCache.Get(cacheKey); ok {
		return cached.([]*core.ScheduleEntry), nil
	}

	for _, p := range s.providers {
		if !s.limiter.Allow(p.Name()) {
			continue
		}
		entries, err := p.GetSchedule(ctx)
		if err != nil {
			log.Printf("provider %s schedule error: %v", p.Name(), err)
			continue
		}
		s.provCache.SetWithTTL(cacheKey, entries, 1*time.Hour)
		return entries, nil
	}
	return nil, fmt.Errorf("no provider available for schedule")
}

// ---- User library (from local store) ----

// ListEntries returns all user entries, optionally filtered by status.
func (s *Service) ListEntries(ctx context.Context, statusFilter ...core.Status) ([]core.UserEntry, error) {
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if len(statusFilter) == 0 {
		return db.Entries, nil
	}
	filtered := make([]core.UserEntry, 0, len(db.Entries))
	for _, e := range db.Entries {
		for _, sf := range statusFilter {
			if e.Status == sf {
				filtered = append(filtered, e)
				break
			}
		}
	}
	return filtered, nil
}

// GetEntry returns a single user entry by UUID or provider:id.
func (s *Service) GetEntry(ctx context.Context, animeID string) (*core.UserEntry, error) {
	uuid, err := s.ResolveID(ctx, animeID)
	if err != nil {
		return nil, nil
	}
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range db.Entries {
		if e.AnimeID == uuid {
			return &e, nil
		}
	}
	return nil, nil
}

// SetStatus updates or creates a user entry for an anime.
// animeID can be a UUID or "provider:id".
func (s *Service) SetStatus(ctx context.Context, animeID string, status core.Status, score int, episodesWatched int) (*core.UserEntry, error) {
	if !status.Valid() {
		return nil, fmt.Errorf("invalid status: %s", status)
	}

	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}

	// Resolve to UUID or create a new AnimeRecord if needed.
	var uuid string
	if providerName, externalID, ok := strings.Cut(animeID, ":"); ok {
		if a := lookupByProviderRef(db, providerName, externalID); a != nil {
			uuid = a.ID
		} else {
			// Need to fetch from provider to create record
			p := provider.Get(providerName)
			if p == nil {
				return nil, fmt.Errorf("unknown provider: %s", providerName)
			}
			a, err := p.GetByID(ctx, externalID)
			if err != nil {
				return nil, fmt.Errorf("fetch %s: %w", animeID, err)
			}
			// Assign UUID and add to DB
			uuid = a.ID
			if uuid == "" {
				uuid = core.CompositeIDToUUID(animeID)
				a.ID = uuid
			}
			db.Anime = append(db.Anime, *a)
		}
	} else {
		uuid = animeID
	}

	now := time.Now().UTC()
	var entry *core.UserEntry

	// Capture old values for history logging.
	var oldStatus core.Status
	var oldScore int
	var oldEpisodes int

	for i := range db.Entries {
		if db.Entries[i].AnimeID == uuid {
			entry = &db.Entries[i]
			oldStatus = entry.Status
			oldScore = entry.Score
			oldEpisodes = entry.EpisodesWatched
			break
		}
	}

	if entry == nil {
		db.Entries = append(db.Entries, core.UserEntry{
			AnimeID:         uuid,
			Status:          status,
			Score:           score,
			EpisodesWatched: episodesWatched,
			StartedAt:       now,
			UpdatedAt:       now,
		})
		if status == core.StatusCompleted {
			db.Entries[len(db.Entries)-1].CompletedAt = now
		}
	} else {
		if entry.Status != status && status == core.StatusCompleted && entry.CompletedAt.IsZero() {
			entry.CompletedAt = now
		}
		if entry.Status != status && status == core.StatusWatching && entry.StartedAt.IsZero() {
			entry.StartedAt = now
		}
		entry.Status = status
		if score > 0 {
			entry.Score = score
		}
		if episodesWatched >= 0 {
			entry.EpisodesWatched = episodesWatched
		}
		entry.UpdatedAt = now
	}

	if err := s.store.Save(ctx, db); err != nil {
		return nil, err
	}

	// Git sync + history in background.
	go func() {
		oldSt := string(oldStatus)
		newSt := string(status)
		// Log history.
		if s.history != nil {
			action := "set_status"
			if oldEpisodes != episodesWatched && episodesWatched >= 0 {
				action = "set_episode"
			}
			s.history.Log(history.Entry{
				AnimeID: uuid,
				Action:  action,
				OldVal:  map[string]interface{}{"status": oldSt, "score": oldScore, "episodes": oldEpisodes},
				NewVal:  map[string]interface{}{"status": newSt, "score": score, "episodes": episodesWatched},
			})
		}
		if s.gitSync != nil {
			msg := fmt.Sprintf("update %s → %s", uuid[:8], status)
			if err := s.gitSync.CommitAndPush(context.Background(), msg); err != nil {
				log.Printf("git sync error: %v", err)
			}
		}
	}()

	for i := range db.Entries {
		if db.Entries[i].AnimeID == uuid {
			return &db.Entries[i], nil
		}
	}
	return nil, fmt.Errorf("entry not found after save")
}

// AddAnimeMeta ensures an anime's metadata exists in the local store.
// Deduplicates by provider ref — if an AnimeRecord with any of the same
// provider refs exists, it updates it instead of creating a duplicate.
func (s *Service) AddAnimeMeta(ctx context.Context, anime *core.AnimeRecord) error {
	db, err := s.store.Load(ctx)
	if err != nil {
		return err
	}

	// Fetch from provider to get full metadata if we have a ref but no data yet.
	var fetched *core.AnimeRecord
	if len(anime.ProviderRefs) > 0 {
		ref := anime.ProviderRefs[0]
		if existing := lookupByProviderRef(db, ref.Provider, ref.ExternalID); existing != nil {
			existing.MergeFrom(anime)
			return s.store.Save(ctx, db)
		}
		// Try fetching fresh data from provider.
		p := provider.Get(ref.Provider)
		if p != nil {
			if f, err := p.GetByID(ctx, ref.ExternalID); err == nil {
				fetched = f
			}
		}
	}

	// Use fetched data if available, otherwise the passed-in record.
	source := anime
	if fetched != nil {
		source = fetched
	}

	// Assign UUID if not set.
	if source.ID == "" {
		if len(source.ProviderRefs) > 0 {
			source.ID = core.CompositeIDToUUID(source.ProviderRefs[0].CompositeID())
		} else {
			source.ID = core.NewAnimeRecord().ID
		}
	}

	db.Anime = append(db.Anime, *source)
	if err := s.store.Save(ctx, db); err != nil {
		return err
	}

	go func() {
		if s.gitSync != nil {
			msg := fmt.Sprintf("add anime: %s", source.Title("en", "ja"))
			if err := s.gitSync.CommitAndPush(context.Background(), msg); err != nil {
				log.Printf("git sync error: %v", err)
			}
		}
	}()

	return nil
}

// GetAnimeMeta retrieves anime metadata from the local store by UUID or provider:id.
func (s *Service) GetAnimeMeta(ctx context.Context, animeID string) (*core.AnimeRecord, error) {
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	// Try direct UUID lookup first.
	for _, a := range db.Anime {
		if a.ID == animeID {
			return &a, nil
		}
	}
	// Try provider:id lookup.
	if pn, eid, ok := strings.Cut(animeID, ":"); ok {
		if a := lookupByProviderRef(db, pn, eid); a != nil {
			return a, nil
		}
	}
	// Fall back to provider API.
	return s.GetAnime(ctx, animeID)
}

// AllAnimeMeta returns all anime metadata in the local store.
func (s *Service) AllAnimeMeta(ctx context.Context) ([]core.AnimeRecord, error) {
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return db.Anime, nil
}

// Stats returns basic statistics about the user's library.
func (s *Service) Stats(ctx context.Context) (map[core.Status]int, error) {
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	stats := make(map[core.Status]int)
	for _, s := range core.AllStatuses {
		stats[s] = 0
	}
	for _, e := range db.Entries {
		stats[e.Status]++
	}
	return stats, nil
}

// ImportMAL imports entries from a MyAnimeList XML export file.
func (s *Service) ImportMAL(ctx context.Context, path string) (*core.ImportResult, error) {
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	imported, err := core.ImportMALXML(path)
	if err != nil {
		return nil, err
	}
	result := core.MergeEntries(db, imported)
	if result.Imported > 0 {
		if err := s.store.Save(ctx, db); err != nil {
			return nil, err
		}
		go func() {
			if s.gitSync != nil {
				s.gitSync.CommitAndPush(context.Background(), fmt.Sprintf("import mal: %d entries", result.Imported))
			}
		}()
	}
	return &result, nil
}

// ImportAniList imports entries from an AniList JSON export file.
func (s *Service) ImportAniList(ctx context.Context, path string) (*core.ImportResult, error) {
	db, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	imported, err := core.ImportAniListJSON(path)
	if err != nil {
		return nil, err
	}
	result := core.MergeEntries(db, imported)
	if result.Imported > 0 {
		if err := s.store.Save(ctx, db); err != nil {
			return nil, err
		}
		go func() {
			if s.gitSync != nil {
				s.gitSync.CommitAndPush(context.Background(), fmt.Sprintf("import anilist: %d entries", result.Imported))
			}
		}()
	}
	return &result, nil
}
