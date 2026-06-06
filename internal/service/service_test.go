package service

import (
	"context"
	"os"
	"testing"

	"github.com/xvantz/animark/internal/core"
)

// addTestAnime is a helper that creates an AnimeRecord for testing.
func addTestAnime(t *testing.T, svc *Service, provider, externalID, titleEN string) {
	t.Helper()
	a := core.NewAnimeRecord()
	a.ProviderRefs = []core.ProviderRef{{Provider: provider, ExternalID: externalID}}
	a.Titles["en"] = titleEN
	a.ID = core.CompositeIDToUUID(provider + ":" + externalID)
	if err := svc.AddAnimeMeta(context.Background(), a); err != nil {
		t.Fatalf("addTestAnime: %v", err)
	}
}

func TestSetStatusAndList(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	addTestAnime(t, svc, "test", "1", "Test Anime 1")

	_, err := svc.SetStatus(ctx, "test:1", core.StatusWatching, 7, 3)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	entries, err := svc.ListEntries(ctx)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	expectedUUID := core.CompositeIDToUUID("test:1")
	if entries[0].AnimeID != expectedUUID {
		t.Errorf("AnimeID = %q, want %q", entries[0].AnimeID, expectedUUID)
	}
	if entries[0].Status != core.StatusWatching {
		t.Errorf("Status = %s, want %s", entries[0].Status, core.StatusWatching)
	}
}

func TestSetStatusUpdates(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	addTestAnime(t, svc, "test", "1", "Test Anime 1")

	_, err := svc.SetStatus(ctx, "test:1", core.StatusWatching, 0, 0)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	_, err = svc.SetStatus(ctx, "test:1", core.StatusCompleted, 9, 12)
	if err != nil {
		t.Fatalf("SetStatus update: %v", err)
	}

	expectedUUID := core.CompositeIDToUUID("test:1")
	entry, err := svc.GetEntry(ctx, expectedUUID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry == nil {
		t.Fatal("entry should exist")
	}
	if entry.Status != core.StatusCompleted {
		t.Errorf("Status = %s, want %s", entry.Status, core.StatusCompleted)
	}
	if entry.Score != 9 {
		t.Errorf("Score = %d, want 9", entry.Score)
	}
	if entry.EpisodesWatched != 12 {
		t.Errorf("EpisodesWatched = %d, want 12", entry.EpisodesWatched)
	}
	if entry.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set when transitioning to completed")
	}
}

func TestListEntriesFilterByStatus(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	addTestAnime(t, svc, "test", "1", "Test 1")
	addTestAnime(t, svc, "test", "2", "Test 2")
	addTestAnime(t, svc, "test", "3", "Test 3")

	svc.SetStatus(ctx, "test:1", core.StatusWatching, 0, 0)
	svc.SetStatus(ctx, "test:2", core.StatusPlanned, 0, 0)
	svc.SetStatus(ctx, "test:3", core.StatusWatching, 0, 0)

	entries, err := svc.ListEntries(ctx, core.StatusWatching)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("watching entries = %d, want 2", len(entries))
	}

	entries, err = svc.ListEntries(ctx, core.StatusPlanned)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("planned entries = %d, want 1", len(entries))
	}

	entries, err = svc.ListEntries(ctx, core.StatusCompleted)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("completed entries = %d, want 0", len(entries))
	}
}

func TestGetEntryNotFound(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	entry, err := svc.GetEntry(ctx, "nonexistent-uuid-here")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry != nil {
		t.Error("expected nil for nonexistent entry")
	}
}

func TestInvalidStatus(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	addTestAnime(t, svc, "test", "1", "Test")

	_, err := svc.SetStatus(ctx, "test:1", core.Status("invalid!"), 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestAddAnimeMeta(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	a := core.NewAnimeRecord()
	a.ProviderRefs = []core.ProviderRef{{Provider: "test", ExternalID: "1"}}
	a.Titles["en"] = "Test Anime"
	a.ID = core.CompositeIDToUUID("test:1")

	if err := svc.AddAnimeMeta(ctx, a); err != nil {
		t.Fatalf("AddAnimeMeta: %v", err)
	}

	meta, err := svc.GetAnimeMeta(ctx, "test:1")
	if err != nil {
		t.Fatalf("GetAnimeMeta: %v", err)
	}
	if meta.Title("en") != "Test Anime" {
		t.Errorf("Title(en) = %q, want %q", meta.Title("en"), "Test Anime")
	}

	all, err := svc.AllAnimeMeta(ctx)
	if err != nil {
		t.Fatalf("AllAnimeMeta: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("all anime = %d, want 1", len(all))
	}
}

func TestStats(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	addTestAnime(t, svc, "test", "1", "T1")
	addTestAnime(t, svc, "test", "2", "T2")
	addTestAnime(t, svc, "test", "3", "T3")
	addTestAnime(t, svc, "test", "4", "T4")

	svc.SetStatus(ctx, "test:1", core.StatusWatching, 0, 0)
	svc.SetStatus(ctx, "test:2", core.StatusWatching, 0, 0)
	svc.SetStatus(ctx, "test:3", core.StatusCompleted, 0, 0)
	svc.SetStatus(ctx, "test:4", core.StatusPlanned, 0, 0)

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if stats[core.StatusWatching] != 2 {
		t.Errorf("watching = %d, want 2", stats[core.StatusWatching])
	}
	if stats[core.StatusCompleted] != 1 {
		t.Errorf("completed = %d, want 1", stats[core.StatusCompleted])
	}
	if stats[core.StatusPlanned] != 1 {
		t.Errorf("planned = %d, want 1", stats[core.StatusPlanned])
	}
	if stats[core.StatusDropped] != 0 {
		t.Errorf("dropped = %d, want 0", stats[core.StatusDropped])
	}
}

// newTestService creates a Service backed by a temp JSONStore with no git sync.
func newTestService(t *testing.T) *Service {
	t.Helper()
	f, err := os.CreateTemp("", "animark-svc-test-*.json")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	f.Close()
	os.Remove(f.Name())

	store := core.NewJSONStore(f.Name())
	t.Cleanup(func() { os.Remove(f.Name()) })

	return New(store, nil)
}
