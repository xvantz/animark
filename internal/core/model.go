// Package core defines the domain models for animark.
package core

import (
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"time"
)

// newUUID generates a random UUID v4 string.
func newUUID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback — should never fail on modern Linux
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			time.Now().UnixNano()&0xffffffff,
			uint16(time.Now().UnixNano()>>16),
			0x4000|uint16(time.Now().UnixNano()>>20)&0x0fff,
			0x8000|uint16(time.Now().UnixNano()>>24)&0x3fff,
			time.Now().UnixNano()>>32)
	}
	// Set version 4 bits
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

// Status represents a user's tracking status for an anime title.
type Status string

const (
	StatusWatching      Status = "watching"
	StatusCompleted     Status = "completed"
	StatusPlanned       Status = "planned"
	StatusOnHold        Status = "on_hold"
	StatusDropped       Status = "dropped"
	StatusNotInterested Status = "not_interested"
	StatusFavorite      Status = "favorite"
)

var AllStatuses = []Status{
	StatusWatching,
	StatusCompleted,
	StatusPlanned,
	StatusOnHold,
	StatusDropped,
	StatusNotInterested,
	StatusFavorite,
}

func (s Status) Valid() bool {
	for _, v := range AllStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// AirStatus describes the real-world release state of an anime.
type AirStatus string

const (
	AirStatusAiring   AirStatus = "airing"
	AirStatusFinished AirStatus = "finished"
	AirStatusUpcoming AirStatus = "upcoming"
	AirStatusUnknown  AirStatus = "unknown"
)

// ProviderRef links an AnimeRecord to an external service identifier.
type ProviderRef struct {
	Provider   string `json:"provider"`   // e.g. "shikimori", "anilist"
	ExternalID string `json:"external_id"` // e.g. "59970"
}

// CompositeID returns the traditional "provider:id" string.
func (r ProviderRef) CompositeID() string {
	return r.Provider + ":" + r.ExternalID
}

// AnimeRecord is the core entity — provider-independent, UUID-keyed.
// Multiple provider refs can point to the same anime, so data survives
// provider switches.
type AnimeRecord struct {
	ID           string            `json:"id"`            // UUID (deterministic from migration, random for new)
	Titles       map[string]string `json:"titles"`         // "ru", "en", "ja", etc.
	ProviderRefs []ProviderRef     `json:"provider_refs"`  // cross-provider links

	ImageURL      string    `json:"image_url,omitempty"`
	Description   string    `json:"description,omitempty"`
	Genres        []string  `json:"genres,omitempty"`
	EpisodesTotal int       `json:"episodes_total"`
	EpisodesAired int       `json:"episodes_aired"`
	AirStatus     AirStatus `json:"air_status"`
	Season        string    `json:"season,omitempty"`
	Year          int       `json:"year,omitempty"`
	Score         float64   `json:"score,omitempty"`
	URL           string    `json:"url,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Title returns the title in the preferred language, falling back.
func (a *AnimeRecord) Title(prefer ...string) string {
	for _, lang := range prefer {
		if t, ok := a.Titles[lang]; ok && t != "" {
			return t
		}
	}
	// Fallback order
	for _, lang := range []string{"ru", "en", "ja"} {
		if t, ok := a.Titles[lang]; ok && t != "" {
			return t
		}
	}
	return "Unknown"
}

// HasProvider checks if this record has a matching provider ref.
func (a *AnimeRecord) HasProvider(provider, externalID string) bool {
	for _, ref := range a.ProviderRefs {
		if ref.Provider == provider && ref.ExternalID == externalID {
			return true
		}
	}
	return false
}

// AddProviderRef adds a provider ref if not already present.
func (a *AnimeRecord) AddProviderRef(ref ProviderRef) {
	if a.HasProvider(ref.Provider, ref.ExternalID) {
		return
	}
	a.ProviderRefs = append(a.ProviderRefs, ref)
}

// DisplayID returns the first composite ID for human-friendly display,
// or the UUID if no provider refs exist.
func (a *AnimeRecord) DisplayID() string {
	for _, ref := range a.ProviderRefs {
		return ref.CompositeID()
	}
	return a.ID
}

// MergeFrom updates this record with data from another record (typically
// from a different provider). The caller's data takes priority for metadata.
func (a *AnimeRecord) MergeFrom(other *AnimeRecord) {
	// Merge titles — prefer existing non-empty, fill missing
	for lang, title := range other.Titles {
		if _, exists := a.Titles[lang]; !exists || a.Titles[lang] == "" {
			a.Titles[lang] = title
		}
	}

	// Merge provider refs
	for _, ref := range other.ProviderRefs {
		a.AddProviderRef(ref)
	}

	// Update metadata — prefer newer data
	if other.Score > 0 && (a.Score == 0 || other.UpdatedAt.After(a.UpdatedAt)) {
		a.Score = other.Score
	}
	if other.EpisodesTotal > 0 {
		a.EpisodesTotal = other.EpisodesTotal
	}
	if other.EpisodesAired > 0 {
		a.EpisodesAired = other.EpisodesAired
	}
	if other.AirStatus != AirStatusUnknown && (a.AirStatus == AirStatusUnknown || other.UpdatedAt.After(a.UpdatedAt)) {
		a.AirStatus = other.AirStatus
	}
	if other.ImageURL != "" {
		a.ImageURL = other.ImageURL
	}
	if other.Description != "" {
		a.Description = other.Description
	}
	if len(other.Genres) > 0 {
		a.Genres = other.Genres
	}
	if other.Season != "" {
		a.Season = other.Season
	}
	if other.Year > 0 {
		a.Year = other.Year
	}
	if other.URL != "" {
		a.URL = other.URL
	}
	a.UpdatedAt = time.Now().UTC()
}

// UserEntry is the user's personal relationship with an anime title.
// AnimeID is the UUID of the AnimeRecord.
type UserEntry struct {
	AnimeID         string    `json:"anime_id"`
	Status          Status    `json:"status"`
	Score           int       `json:"score"`            // 0-10
	EpisodesWatched int       `json:"episodes_watched"`
	Notes           string    `json:"notes,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// UserDatabase is the top-level structure stored in anime.json and committed to git.
type UserDatabase struct {
	Version int           `json:"version"`
	Updated time.Time     `json:"updated"`
	Entries []UserEntry   `json:"entries"`
	Anime   []AnimeRecord `json:"anime"`
}

// ScheduleEntry represents a scheduled broadcast.
type ScheduleEntry struct {
	AnimeID   string `json:"anime_id"`
	Provider  string `json:"provider"`
	Episode   int    `json:"episode"`
	AirTime   string `json:"air_time"` // ISO 8601 or relative
	DayOfWeek string `json:"day_of_week"`
}

// LegacyAnime is the old v1 format (composite IDs). Only used during migration.
type LegacyAnime struct {
	ID             string    `json:"id"`
	Provider       string    `json:"provider"`
	ExternalID     string    `json:"external_id"`
	TitleRU        string    `json:"title_ru,omitempty"`
	TitleEN        string    `json:"title_en,omitempty"`
	TitleJP        string    `json:"title_jp,omitempty"`
	Synonyms       []string  `json:"synonyms,omitempty"`
	ImageURL       string    `json:"image_url,omitempty"`
	Description    string    `json:"description,omitempty"`
	Genres         []string  `json:"genres,omitempty"`
	EpisodesTotal  int       `json:"episodes_total"`
	EpisodesAired  int       `json:"episodes_aired"`
	AirStatus      AirStatus `json:"air_status"`
	Season         string    `json:"season,omitempty"`
	Year           int       `json:"year,omitempty"`
	Score          float64   `json:"score,omitempty"`
	URL            string    `json:"url,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// LegacyUserDatabase is the old v1 format. Used to detect and migrate.
type LegacyUserDatabase struct {
	Version int          `json:"version"`
	Updated time.Time    `json:"updated"`
	Entries []UserEntry  `json:"entries"`
	Anime   []LegacyAnime `json:"anime"`
}

// CompositeIDToUUID produces a deterministic UUID-like ID from a composite ID.
// Used for stable migration from v1 (provider:id) to v2 (UUID).
func CompositeIDToUUID(compositeID string) string {
	h := sha1.Sum([]byte("animark:" + compositeID))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// NewAnimeRecord generates a random UUID for a new anime entry.
func NewAnimeRecord() *AnimeRecord {
	return &AnimeRecord{
		ID:           newUUID(),
		Titles:       make(map[string]string),
		ProviderRefs: make([]ProviderRef, 0),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

// ToAnimeRecord converts a legacy anime to the new format, assigning a UUID.
func (l *LegacyAnime) ToAnimeRecord() *AnimeRecord {
	uuid := CompositeIDToUUID(l.ID)
	record := &AnimeRecord{
		ID: uuid,
		Titles: map[string]string{
			"en": l.TitleEN,
			"ja": l.TitleJP,
		},
		ProviderRefs: []ProviderRef{
			{Provider: l.Provider, ExternalID: l.ExternalID},
		},
		ImageURL:      l.ImageURL,
		Description:   l.Description,
		Genres:        l.Genres,
		EpisodesTotal: l.EpisodesTotal,
		EpisodesAired: l.EpisodesAired,
		AirStatus:     l.AirStatus,
		Season:        l.Season,
		Year:          l.Year,
		Score:         l.Score,
		URL:           l.URL,
		CreatedAt:     l.CreatedAt,
		UpdatedAt:     l.UpdatedAt,
	}
	if l.TitleRU != "" {
		record.Titles["ru"] = l.TitleRU
	}
	return record
}

// LegacyUserDatabaseToV2 migrates a v1 database to v2.
// Returns a map of old compositeID → new UUID for updating entries.
func LegacyUserDatabaseToV2(old *LegacyUserDatabase) (*UserDatabase, map[string]string) {
	db := &UserDatabase{
		Version: 2,
		Updated: old.Updated,
		Entries: make([]UserEntry, 0, len(old.Entries)),
		Anime:   make([]AnimeRecord, 0, len(old.Anime)),
	}

	idMap := make(map[string]string, len(old.Anime))

	// Convert anime records
	for _, a := range old.Anime {
		record := a.ToAnimeRecord()
		idMap[a.ID] = record.ID
		db.Anime = append(db.Anime, *record)
	}

	// Convert entries — remap AnimeID from composite to UUID
	for _, e := range old.Entries {
		if newID, ok := idMap[e.AnimeID]; ok {
			e.AnimeID = newID
		}
		db.Entries = append(db.Entries, e)
	}

	return db, idMap
}
