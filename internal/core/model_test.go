package core

import (
	"testing"
)

func TestStatusValid(t *testing.T) {
	tests := []struct {
		s     Status
		valid bool
	}{
		{StatusWatching, true},
		{StatusCompleted, true},
		{StatusPlanned, true},
		{StatusOnHold, true},
		{StatusDropped, true},
		{StatusNotInterested, true},
		{StatusFavorite, true},
		{Status("unknown"), false},
		{Status(""), false},
	}

	for _, tc := range tests {
		got := tc.s.Valid()
		if got != tc.valid {
			t.Errorf("Status(%q).Valid() = %v, want %v", tc.s, got, tc.valid)
		}
	}
}

func TestAllStatusesCount(t *testing.T) {
	if len(AllStatuses) != 7 {
		t.Errorf("AllStatuses has %d entries, want 7", len(AllStatuses))
	}
}

func TestAnimeRecordDefaults(t *testing.T) {
	a := NewAnimeRecord()
	a.ProviderRefs = []ProviderRef{{Provider: "test", ExternalID: "1"}}
	a.ID = CompositeIDToUUID("test:1")
	a.Titles["en"] = "Test Anime"

	if a.ID == "" {
		t.Error("ID should be set")
	}
	if a.Title("en") != "Test Anime" {
		t.Errorf("Title(en) = %q, want Test Anime", a.Title("en"))
	}
	if a.Genres != nil {
		t.Error("Genres should be nil by default")
	}
}

func TestAirStatusValues(t *testing.T) {
	if AirStatusAiring != "airing" {
		t.Errorf("AirStatusAiring = %q", AirStatusAiring)
	}
	if AirStatusFinished != "finished" {
		t.Errorf("AirStatusFinished = %q", AirStatusFinished)
	}
	if AirStatusUpcoming != "upcoming" {
		t.Errorf("AirStatusUpcoming = %q", AirStatusUpcoming)
	}
	if AirStatusUnknown != "unknown" {
		t.Errorf("AirStatusUnknown = %q", AirStatusUnknown)
	}
}

func TestUserDatabaseDefaults(t *testing.T) {
	db := &UserDatabase{
		Version: 2,
	}
	if db.Version != 2 {
		t.Errorf("Version = %d, want 2", db.Version)
	}
}

func TestUserEntryStatusTransition(t *testing.T) {
	uuid := CompositeIDToUUID("test:1")
	entry := UserEntry{AnimeID: uuid, Status: StatusPlanned}

	if entry.Status != StatusPlanned {
		t.Errorf("initial status = %s, want %s", entry.Status, StatusPlanned)
	}

	entry.Status = StatusWatching
	if entry.Status != StatusWatching {
		t.Errorf("updated status = %s, want %s", entry.Status, StatusWatching)
	}

	entry.Status = StatusCompleted
	if entry.Status != StatusCompleted {
		t.Errorf("final status = %s, want %s", entry.Status, StatusCompleted)
	}
}

func TestScheduleEntry(t *testing.T) {
	e := ScheduleEntry{
		AnimeID:   "test:1",
		Episode:   5,
		DayOfWeek: "Monday",
		AirTime:   "2026-06-08T00:00:00Z",
	}

	if e.Episode != 5 {
		t.Errorf("Episode = %d, want 5", e.Episode)
	}
	if e.DayOfWeek != "Monday" {
		t.Errorf("DayOfWeek = %q, want Monday", e.DayOfWeek)
	}
}

func TestCompositeIDToUUID(t *testing.T) {
	uuid := CompositeIDToUUID("test:1")
	if len(uuid) != 36 {
		t.Errorf("UUID length = %d, want 36", len(uuid))
	}
	// Deterministic
	uuid2 := CompositeIDToUUID("test:1")
	if uuid != uuid2 {
		t.Errorf("not deterministic: %q != %q", uuid, uuid2)
	}
}

func TestAnimeRecordMergeFrom(t *testing.T) {
	a := NewAnimeRecord()
	a.Titles["en"] = "Original Title"
	a.ProviderRefs = []ProviderRef{{Provider: "shikimori", ExternalID: "1"}}
	a.Score = 8.5
	a.EpisodesTotal = 24

	b := NewAnimeRecord()
	b.Titles["en"] = "New Title"   // should NOT override
	b.Titles["ja"] = "Japanese Title"  // should be added
	b.ProviderRefs = []ProviderRef{{Provider: "anilist", ExternalID: "100"}}
	b.Score = 9.0  // higher, should update
	b.EpisodesTotal = 26  // higher, should update

	a.MergeFrom(b)

	if a.Title("en") != "Original Title" {
		t.Errorf("Title(en) overridden: %q", a.Title("en"))
	}
	if a.Title("ja") != "Japanese Title" {
		t.Errorf("Title(ja) missing: %q", a.Title("ja"))
	}
	if !a.HasProvider("anilist", "100") {
		t.Error("anilist:100 not found after merge")
	}
	if !a.HasProvider("shikimori", "1") {
		t.Error("shikimori:1 not found after merge")
	}
	if a.Score != 9.0 {
		t.Errorf("Score = %.1f, want 9.0", a.Score)
	}
	if a.EpisodesTotal != 26 {
		t.Errorf("EpisodesTotal = %d, want 26", a.EpisodesTotal)
	}
}

func TestDisplayID(t *testing.T) {
	a := NewAnimeRecord()
	a.ProviderRefs = []ProviderRef{{Provider: "shikimori", ExternalID: "59970"}}
	expected := "shikimori:59970"
	if a.DisplayID() != expected {
		t.Errorf("DisplayID = %q, want %q", a.DisplayID(), expected)
	}
}

func TestLegacyAnimeToAnimeRecord(t *testing.T) {
	old := LegacyAnime{
		ID:            "shikimori:59970",
		Provider:      "shikimori",
		ExternalID:    "59970",
		TitleEN:       "Test Anime",
		TitleRU:       "Тестовое Аниме",
		EpisodesTotal: 12,
		Score:         8.5,
	}

	record := old.ToAnimeRecord()
	if record.ID != CompositeIDToUUID("shikimori:59970") {
		t.Errorf("UUID mismatch: %q", record.ID)
	}
	if record.Title("en") != "Test Anime" {
		t.Errorf("Title(en) = %q", record.Title("en"))
	}
	if record.Title("ru") != "Тестовое Аниме" {
		t.Errorf("Title(ru) = %q", record.Title("ru"))
	}
	if len(record.ProviderRefs) != 1 {
		t.Errorf("ProviderRefs = %d, want 1", len(record.ProviderRefs))
	}
	if record.ProviderRefs[0].Provider != "shikimori" {
		t.Errorf("Provider = %q", record.ProviderRefs[0].Provider)
	}
}

func TestLegacyUserDatabaseToV2(t *testing.T) {
	old := LegacyUserDatabase{
		Version: 1,
		Entries: []UserEntry{
			{AnimeID: "shikimori:59970", Status: StatusWatching},
		},
		Anime: []LegacyAnime{
			{
				ID:         "shikimori:59970",
				Provider:   "shikimori",
				ExternalID: "59970",
				TitleEN:    "Test",
			},
		},
	}

	db, idMap := LegacyUserDatabaseToV2(&old)
	if db.Version != 2 {
		t.Errorf("Version = %d, want 2", db.Version)
	}
	if len(db.Anime) != 1 {
		t.Errorf("Anime records = %d, want 1", len(db.Anime))
	}
	newUUID := idMap["shikimori:59970"]
	if newUUID == "" {
		t.Error("missing mapping for shikimori:59970")
	}
	if db.Entries[0].AnimeID != newUUID {
		t.Errorf("Entry AnimeID = %q, want %q", db.Entries[0].AnimeID, newUUID)
	}
	if db.Anime[0].Titles["en"] != "Test" {
		t.Errorf("Title(en) = %q", db.Anime[0].Titles["en"])
	}
}
