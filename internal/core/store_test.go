package core

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestJSONStoreCreateNew(t *testing.T) {
	path := tempFile(t)
	defer os.Remove(path)
	os.Remove(path) // Remove file so Load creates a fresh DB

	s := NewJSONStore(path)
	db, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load on new store: %v", err)
	}
	if db.Version != 2 {
		t.Errorf("Version = %d, want 2", db.Version)
	}
	if len(db.Entries) != 0 {
		t.Errorf("Entries len = %d, want 0", len(db.Entries))
	}
	if len(db.Anime) != 0 {
		t.Errorf("Anime len = %d, want 0", len(db.Anime))
	}
}

func TestJSONStoreSaveAndLoad(t *testing.T) {
	path := tempFile(t)
	defer os.Remove(path)

	s := NewJSONStore(path)

	uuid1 := CompositeIDToUUID("test:1")
	uuid2 := CompositeIDToUUID("test:2")

	db := &UserDatabase{
		Version: 2,
		Entries: []UserEntry{
			{AnimeID: uuid1, Status: StatusWatching, Score: 8, EpisodesWatched: 5},
			{AnimeID: uuid2, Status: StatusPlanned, Score: 0},
		},
		Anime: []AnimeRecord{
			{
				ID:     uuid1,
				Titles: map[string]string{"en": "Anime One"},
				ProviderRefs: []ProviderRef{
					{Provider: "test", ExternalID: "1"},
				},
				EpisodesTotal: 24,
			},
		},
	}

	if err := s.Save(context.Background(), db); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}

	if loaded.Version != 2 {
		t.Errorf("Version = %d, want 2", loaded.Version)
	}
	if len(loaded.Entries) != 2 {
		t.Errorf("Entries = %d, want 2", len(loaded.Entries))
	}
	if loaded.Entries[0].AnimeID != uuid1 {
		t.Errorf("Entry[0].AnimeID = %q, want %q", loaded.Entries[0].AnimeID, uuid1)
	}

	// Check sorting.
	if loaded.Entries[0].AnimeID > loaded.Entries[1].AnimeID {
		t.Errorf("Entries not sorted: %v", loaded.Entries)
	}

	// Check anime metadata.
	rec := loaded.Anime[0]
	if rec.Title("en") != "Anime One" {
		t.Errorf("Anime[0].Title(en) = %q, want Anime One", rec.Title("en"))
	}
	if rec.EpisodesTotal != 24 {
		t.Errorf("EpisodesTotal = %d, want 24", rec.EpisodesTotal)
	}
	if len(rec.ProviderRefs) != 1 || rec.ProviderRefs[0].Provider != "test" {
		t.Errorf("ProviderRefs mismatch: %+v", rec.ProviderRefs)
	}
}

func TestJSONStoreRoundTripEmpty(t *testing.T) {
	path := tempFile(t)
	defer os.Remove(path)

	s := NewJSONStore(path)

	db1 := &UserDatabase{Version: 2}
	if err := s.Save(context.Background(), db1); err != nil {
		t.Fatalf("Save empty: %v", err)
	}

	db2, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(db2.Entries) != 0 {
		t.Errorf("Entries = %d, want 0", len(db2.Entries))
	}
}

func TestJSONStoreAtomicity(t *testing.T) {
	path := tempFile(t)
	defer os.Remove(path)

	s := NewJSONStore(path)

	uuid1 := CompositeIDToUUID("test:1")

	db := &UserDatabase{
		Version: 2,
		Entries: []UserEntry{{AnimeID: uuid1, Status: StatusWatching}},
	}
	if err := s.Save(context.Background(), db); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Errorf("Entries = %d, want 1", len(loaded.Entries))
	}
	if loaded.Entries[0].AnimeID != uuid1 {
		t.Errorf("Entry.AnimeID = %q, want %q", loaded.Entries[0].AnimeID, uuid1)
	}
}

func TestJSONStorePath(t *testing.T) {
	s := NewJSONStore("/tmp/test/anime.json")
	if s.Path() != "/tmp/test/anime.json" {
		t.Errorf("Path() = %q, want /tmp/test/anime.json", s.Path())
	}
}

func TestJSONStoreMigrationV1ToV2(t *testing.T) {
	path := tempFile(t)
	defer os.Remove(path)

	// Write a v1-format file manually.
	v1Data := `{
  "version": 1,
  "entries": [
    {"anime_id": "shikimori:59970", "status": "watching", "score": 8, "episodes_watched": 5}
  ],
  "anime": [
    {"id": "shikimori:59970", "provider": "shikimori", "external_id": "59970", "title_en": "Test Anime", "episodes_total": 12}
  ]
}`
	if err := os.WriteFile(path, []byte(v1Data), 0644); err != nil {
		t.Fatalf("write v1 data: %v", err)
	}

	s := NewJSONStore(path)
	db, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load migrated: %v", err)
	}

	if db.Version != 2 {
		t.Errorf("Version = %d, want 2 after migration", db.Version)
	}
	if len(db.Entries) != 1 {
		t.Errorf("Entries = %d, want 1", len(db.Entries))
	}
	if len(db.Anime) != 1 {
		t.Errorf("Anime = %d, want 1", len(db.Anime))
	}

	// Check UUID remapping.
	expectedUUID := CompositeIDToUUID("shikimori:59970")
	if db.Entries[0].AnimeID != expectedUUID {
		t.Errorf("Entry AnimeID = %q, want %q", db.Entries[0].AnimeID, expectedUUID)
	}
	if db.Anime[0].ID != expectedUUID {
		t.Errorf("Anime[0].ID = %q, want %q", db.Anime[0].ID, expectedUUID)
	}
	if db.Anime[0].Title("en") != "Test Anime" {
		t.Errorf("Title(en) = %q", db.Anime[0].Title("en"))
	}
	if len(db.Anime[0].ProviderRefs) != 1 {
		t.Errorf("ProviderRefs = %d, want 1", len(db.Anime[0].ProviderRefs))
	}

	// Verify file was rewritten on disk.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	var versionCheck struct{ Version int }
	if err := json.Unmarshal(raw, &versionCheck); err != nil {
		t.Fatalf("parse migrated: %v", err)
	}
	if versionCheck.Version != 2 {
		t.Errorf("On-disk version = %d, want 2", versionCheck.Version)
	}
}

func tempFile(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "animark-test-*.json")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	f.Close()
	return f.Name()
}
