package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store is the persistence layer for user data.
type Store interface {
	Load(ctx context.Context) (*UserDatabase, error)
	Save(ctx context.Context, db *UserDatabase) error
	Path() string
}

// JSONStore implements Store using a single JSON file with automatic
// migration from v1 (composite IDs) to v2 (UUIDs).
type JSONStore struct {
	mu     sync.RWMutex
	path   string
	pretty bool
}

func NewJSONStore(path string) *JSONStore {
	return &JSONStore{
		path:   path,
		pretty: true,
	}
}

func (s *JSONStore) Path() string { return s.path }

func (s *JSONStore) Load(_ context.Context) (*UserDatabase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return &UserDatabase{
			Version: 2,
			Updated: time.Now().UTC(),
			Entries: []UserEntry{},
			Anime:   []AnimeRecord{},
		}, nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read store: %w", err)
	}

	// Detect version by trying to parse just the version field.
	var versionCheck struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &versionCheck); err != nil {
		return nil, fmt.Errorf("parse store version: %w", err)
	}

	if versionCheck.Version >= 2 {
		return s.loadV2(data)
	}

	// v1 → v2 migration
	db, err := s.migrateV1toV2(data)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (s *JSONStore) loadV2(data []byte) (*UserDatabase, error) {
	var db UserDatabase
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("parse store v2: %w", err)
	}
	if db.Entries == nil {
		db.Entries = []UserEntry{}
	}
	if db.Anime == nil {
		db.Anime = []AnimeRecord{}
	}

	return &db, nil
}

func (s *JSONStore) migrateV1toV2(data []byte) (*UserDatabase, error) {
	var old LegacyUserDatabase
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, fmt.Errorf("parse store v1: %w", err)
	}
	if old.Anime == nil {
		old.Anime = []LegacyAnime{}
	}
	if old.Entries == nil {
		old.Entries = []UserEntry{}
	}

	db, _ := LegacyUserDatabaseToV2(&old)

	// Write back the migrated data.
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.RLock()
	defer s.mu.Unlock()

	db.Updated = time.Now().UTC()
	sort.Slice(db.Entries, func(i, j int) bool {
		return db.Entries[i].AnimeID < db.Entries[j].AnimeID
	})
	sort.Slice(db.Anime, func(i, j int) bool {
		return db.Anime[i].ID < db.Anime[j].ID
	})

	out, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode migrated store: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0644); err != nil {
		return nil, fmt.Errorf("write migrated store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return nil, fmt.Errorf("rename migrated store: %w", err)
	}

	return db, nil
}

func (s *JSONStore) Save(_ context.Context, db *UserDatabase) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	db.Version = 2
	db.Updated = time.Now().UTC()

	// Sort entries by anime ID for deterministic diffs.
	sort.Slice(db.Entries, func(i, j int) bool {
		return db.Entries[i].AnimeID < db.Entries[j].AnimeID
	})
	sort.Slice(db.Anime, func(i, j int) bool {
		return db.Anime[i].ID < db.Anime[j].ID
	})

	var (
		data []byte
		err  error
	)
	if s.pretty {
		data, err = json.MarshalIndent(db, "", "  ")
	} else {
		data, err = json.Marshal(db)
	}
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir store: %w", err)
	}

	// Atomic write: write to tmp, then rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename store: %w", err)
	}

	return nil
}
