package core

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"time"
)

// ImportResult describes what was imported.
type ImportResult struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors"`
}

// ImportEntry is a single imported item, carrying both the user entry and
// enough anime metadata to create or update an AnimeRecord.
type ImportEntry struct {
	Entry   UserEntry
	Anime   *AnimeRecord // partial record with Titles and ProviderRefs
}

// ---- MAL XML format ----

type malAnimeList struct {
	XMLName xml.Name   `xml:"myanimelist"`
	Anime   []malAnime `xml:"anime"`
}

type malAnime struct {
	SeriesAnimeDBID  int    `xml:"series_animedb_id"`
	SeriesTitle      string `xml:"series_title"`
	MyStatus         int    `xml:"my_status"`
	MyScore          int    `xml:"my_score"`
	MyWatchedEp      int    `xml:"my_watched_episodes"`
	MyStartDate      string `xml:"my_start_date"`
	MyFinishDate     string `xml:"my_finish_date"`
	MyRewatching     int    `xml:"my_rewatching"`
}

func malStatus(s int) Status {
	switch s {
	case 1:
		return StatusWatching
	case 2:
		return StatusCompleted
	case 3:
		return StatusOnHold
	case 4:
		return StatusDropped
	case 6:
		return StatusPlanned
	default:
		return StatusNotInterested
	}
}

// ImportMALXML parses a MyAnimeList export XML and returns import entries.
func ImportMALXML(path string) ([]ImportEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mal xml: %w", err)
	}

	var list malAnimeList
	if err := xml.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse mal xml: %w", err)
	}

	now := time.Now().UTC()
	entries := make([]ImportEntry, 0, len(list.Anime))

	for _, a := range list.Anime {
		if a.SeriesAnimeDBID == 0 {
			continue
		}
		compositeID := fmt.Sprintf("mal:%d", a.SeriesAnimeDBID)
		uuid := CompositeIDToUUID(compositeID)

		e := UserEntry{
			AnimeID:         uuid,
			Status:          malStatus(a.MyStatus),
			Score:           a.MyScore,
			EpisodesWatched: a.MyWatchedEp,
			UpdatedAt:       now,
		}
		if a.MyStartDate != "0000-00-00" && a.MyStartDate != "" {
			if t, err := time.Parse("2006-01-02", a.MyStartDate); err == nil {
				e.StartedAt = t
			}
		}
		if a.MyFinishDate != "0000-00-00" && a.MyFinishDate != "" {
			if t, err := time.Parse("2006-01-02", a.MyFinishDate); err == nil {
				e.CompletedAt = t
				if e.Status == StatusPlanned {
					e.Status = StatusCompleted
				}
			}
		}

		anime := NewAnimeRecord()
		anime.ID = uuid
		anime.Titles["en"] = a.SeriesTitle
		anime.ProviderRefs = []ProviderRef{
			{Provider: "mal", ExternalID: fmt.Sprintf("%d", a.SeriesAnimeDBID)},
		}

		entries = append(entries, ImportEntry{Entry: e, Anime: anime})
	}

	return entries, nil
}

// ---- AniList JSON format ----

type anilistExport struct {
	Lists []anilistList `json:"lists"`
}

type anilistList struct {
	Name    string         `json:"name"`
	Entries []anilistEntry `json:"entries"`
}

type anilistEntry struct {
	Media       anilistMedia `json:"media"`
	Score       int          `json:"score"`
	Progress    int          `json:"progress"`
	Status      string       `json:"status"`
	StartedAt   anilistDate  `json:"startedAt"`
	CompletedAt anilistDate  `json:"completedAt"`
}

type anilistMedia struct {
	ID    int          `json:"id"`
	Title anilistTitle `json:"title"`
}

type anilistTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
}

type anilistDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func anilistStatus(listName, status string) Status {
	switch status {
	case "CURRENT":
		return StatusWatching
	case "COMPLETED":
		return StatusCompleted
	case "PLANNING":
		return StatusPlanned
	case "PAUSED":
		return StatusOnHold
	case "DROPPED":
		return StatusDropped
	case "REPEATING":
		return StatusFavorite
	}
	switch listName {
	case "Watching", "Current":
		return StatusWatching
	case "Completed":
		return StatusCompleted
	case "Planning", "Plan to Watch":
		return StatusPlanned
	case "Paused":
		return StatusOnHold
	case "Dropped":
		return StatusDropped
	case "Favorites", "Favourite":
		return StatusFavorite
	default:
		return StatusPlanned
	}
}

// ImportAniListJSON parses an AniList export JSON and returns import entries.
func ImportAniListJSON(path string) ([]ImportEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read anilist json: %w", err)
	}

	var export anilistExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parse anilist json: %w", err)
	}

	now := time.Now().UTC()
	entries := make([]ImportEntry, 0)

	for _, list := range export.Lists {
		for _, e := range list.Entries {
			if e.Media.ID == 0 {
				continue
			}
			compositeID := fmt.Sprintf("anilist:%d", e.Media.ID)
			uuid := CompositeIDToUUID(compositeID)

			entry := UserEntry{
				AnimeID:         uuid,
				Status:          anilistStatus(list.Name, e.Status),
				Score:           e.Score / 10,
				EpisodesWatched: e.Progress,
				UpdatedAt:       now,
			}
			if e.StartedAt.Year > 0 {
				entry.StartedAt = time.Date(e.StartedAt.Year, time.Month(e.StartedAt.Month), e.StartedAt.Day, 0, 0, 0, 0, time.UTC)
			}
			if e.CompletedAt.Year > 0 {
				entry.CompletedAt = time.Date(e.CompletedAt.Year, time.Month(e.CompletedAt.Month), e.CompletedAt.Day, 0, 0, 0, 0, time.UTC)
			}

			anime := NewAnimeRecord()
			anime.ID = uuid
			if e.Media.Title.English != "" {
				anime.Titles["en"] = e.Media.Title.English
			}
			if e.Media.Title.Romaji != "" {
				anime.Titles["ja"] = e.Media.Title.Romaji
			}
			anime.ProviderRefs = []ProviderRef{
				{Provider: "anilist", ExternalID: fmt.Sprintf("%d", e.Media.ID)},
			}

			entries = append(entries, ImportEntry{Entry: entry, Anime: anime})
		}
	}

	return entries, nil
}

// ---- Merge ----

// MergeEntries merges imported entries into the existing database.
// For each entry, if the anime (by UUID) already exists, skip it.
// If the anime exists with matching provider ref, re-use the UUID.
// Otherwise create new AnimeRecord.
func MergeEntries(existing *UserDatabase, imported []ImportEntry) ImportResult {
	result := ImportResult{Total: len(imported)}

	existingByUUID := make(map[string]int)
	for i, e := range existing.Entries {
		existingByUUID[e.AnimeID] = i
	}

	existingAnime := make(map[string]int)   // UUID → index
	animeByProviderRef := make(map[string]int) // provider:id → anime index
	for i, a := range existing.Anime {
		existingAnime[a.ID] = i
		for _, ref := range a.ProviderRefs {
			animeByProviderRef[ref.CompositeID()] = i
		}
	}

	for _, ie := range imported {
		entry := ie.Entry
		anime := ie.Anime

		// Find or create AnimeRecord
		if idx, ok := existingAnime[entry.AnimeID]; ok {
			// UUID match — merge metadata
			existing.Anime[idx].MergeFrom(anime)
		} else if len(anime.ProviderRefs) > 0 {
			ref := anime.ProviderRefs[0].CompositeID()
			if idx, ok := animeByProviderRef[ref]; ok {
				// Same anime, different UUID — merge and remap
				oldID := existing.Anime[idx].ID
				existing.Anime[idx].MergeFrom(anime)
				entry.AnimeID = oldID
			} else {
				// New anime — add it
				existing.Anime = append(existing.Anime, *anime)
				existingAnime[anime.ID] = len(existing.Anime) - 1
				animeByProviderRef[ref] = len(existing.Anime) - 1
			}
		} else {
			// No provider refs — just add
			existing.Anime = append(existing.Anime, *anime)
			existingAnime[anime.ID] = len(existing.Anime) - 1
		}

		// Find or create UserEntry
		if idx, ok := existingByUUID[entry.AnimeID]; ok {
			// Only update if imported entry is newer
			if !entry.UpdatedAt.After(existing.Entries[idx].UpdatedAt) {
				result.Skipped++
				continue
			}
			existing.Entries[idx] = entry
			result.Imported++
		} else {
			existing.Entries = append(existing.Entries, entry)
			existingByUUID[entry.AnimeID] = len(existing.Entries) - 1
			result.Imported++
		}
	}

	return result
}
