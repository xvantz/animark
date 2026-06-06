package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/xvantz/animark/internal/core"
	"github.com/xvantz/animark/internal/provider"
	"github.com/xvantz/animark/internal/provider/shikimori"
	"github.com/xvantz/animark/internal/service"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	provider.Register(shikimori.NewClient())
	f, err := os.CreateTemp("", "animark-api-test-*.json")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	f.Close()
	os.Remove(f.Name())

	store := core.NewJSONStore(f.Name())
	svc := service.New(store, nil)
	srv, err := NewServer(svc)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	return srv
}

func TestIndexPage(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "animark") {
		t.Error("body should contain 'animark'")
	}
	if !strings.Contains(body, "Library") {
		t.Error("body should contain 'Library'")
	}
	if !strings.Contains(body, "htmx.org") {
		t.Error("body should contain htmx script tag")
	}
}

func TestSearchPage(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Search") {
		t.Error("body should contain Search")
	}
}

func TestSeasonalPage(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/seasonal", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Seasonal") {
		t.Error("body should contain Seasonal")
	}
}

func TestAPISearch(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Naruto", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var results []*core.AnimeRecord
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("decode results: %v", err)
	}
	if len(results) == 0 {
		t.Log("no results from shikimori (possibly offline or rate-limited)")
	} else {
		if len(results[0].ProviderRefs) == 0 {
			t.Error("result should have at least one provider ref")
		} else if results[0].ProviderRefs[0].Provider != "shikimori" {
			t.Errorf("provider = %q, want shikimori", results[0].ProviderRefs[0].Provider)
		}
		if results[0].Title("en") == "" && results[0].Title("ru") == "" {
			t.Error("result should have a title")
		}
	}
}

func TestAPISearchNoQuery(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIStats(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var stats map[string]int
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	for _, s := range core.AllStatuses {
		if _, ok := stats[string(s)]; !ok {
			t.Errorf("missing status: %s", s)
		}
	}
}

func TestAPISetStatus(t *testing.T) {
	srv := newTestServer(t)

	// Add anime metadata first using new format (AnimeRecord with ProviderRefs).
	animeID := core.CompositeIDToUUID("test:1")
	animePayload := `{"id":"` + animeID + `","titles":{"en":"Test Anime"},"provider_refs":[{"provider":"test","external_id":"1"}],"episodes_total":12}`
	req := httptest.NewRequest(http.MethodPost, "/api/anime/test:1", strings.NewReader(animePayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("anime create status = %d, want %d", w.Code, http.StatusCreated)
	}

	// Set status using the composite ID.
	statusPayload := `{"status":"watching","score":8,"episodes_watched":3}`
	req = httptest.NewRequest(http.MethodPut, "/api/entries/test:1", strings.NewReader(statusPayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("set status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var entry core.UserEntry
	if err := json.NewDecoder(w.Body).Decode(&entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	if entry.AnimeID != animeID {
		t.Errorf("AnimeID = %q, want %q", entry.AnimeID, animeID)
	}
	if entry.Status != core.StatusWatching {
		t.Errorf("Status = %s, want %s", entry.Status, core.StatusWatching)
	}
	if entry.Score != 8 {
		t.Errorf("Score = %d, want 8", entry.Score)
	}
	if entry.EpisodesWatched != 3 {
		t.Errorf("EpisodesWatched = %d, want 3", entry.EpisodesWatched)
	}
}

func TestAPIGetEntries(t *testing.T) {
	srv := newTestServer(t)

	// Pre-create anime and set status.
	animeID := core.CompositeIDToUUID("test:99")
	animePayload := `{"id":"` + animeID + `","titles":{"en":"Test Anime 99"},"provider_refs":[{"provider":"test","external_id":"99"}],"episodes_total":24}`
	req := httptest.NewRequest(http.MethodPost, "/api/anime/test:99", strings.NewReader(animePayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("anime create: %d", w.Code)
	}

	statusPayload := `{"status":"completed","score":10,"episodes_watched":24}`
	req = httptest.NewRequest(http.MethodPut, "/api/entries/test:99", strings.NewReader(statusPayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set status: %d", w.Code)
	}

	// Get all entries.
	req = httptest.NewRequest(http.MethodGet, "/api/entries", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var entries []core.UserEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].AnimeID != animeID {
		t.Errorf("AnimeID = %q, want %q", entries[0].AnimeID, animeID)
	}
}

func TestAPINotFound(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/anime/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/search", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestAnimeDetailPageNotFound(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/anime/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
