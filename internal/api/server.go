// Package api provides HTTP handlers and routes.
package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/xvantz/animark/internal/core"
	"github.com/xvantz/animark/internal/history"
	"github.com/xvantz/animark/internal/service"
	"github.com/xvantz/animark/internal/stream"
)
// Server holds HTTP dependencies.
type Server struct {
	svc          *service.Service
	mux          *http.ServeMux
	layout       *template.Template
	historyPath  string
	streamNames  []string
}

// NewServer creates an HTTP server with routes and templates.
func NewServer(svc *service.Service) (*Server, error) {
	s := &Server{
		svc: svc,
		mux: http.NewServeMux(),
	}

	tmpl, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}
	s.layout = tmpl

	s.registerRoutes()
	return s, nil
}

// WithHistoryPath attaches a history JSONL path for the stats page.
func (s *Server) WithHistoryPath(path string) *Server {
	s.historyPath = path
	return s
}

// WithStreams attaches registered stream provider names for the player UI.
func (s *Server) WithStreams(names []string) *Server {
	s.streamNames = names
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	// Web pages
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/search", s.handleSearch)
	s.mux.HandleFunc("/anime/", s.handleAnimeDetail)
	s.mux.HandleFunc("/seasonal", s.handleSeasonal)
	s.mux.HandleFunc("/top", s.handleTop)
	s.mux.HandleFunc("/stats", s.handleStatsPage)

	// API endpoints
	s.mux.HandleFunc("/api/anime", s.handleAPIAnime)
	s.mux.HandleFunc("/api/anime/", s.handleAPIAnimeByID)
	s.mux.HandleFunc("/api/entries", s.handleAPIEntries)
	s.mux.HandleFunc("/api/entries/", s.handleAPIEntryByID)
	s.mux.HandleFunc("/api/search", s.handleAPISearch)
	s.mux.HandleFunc("/api/stats", s.handleAPIStats)
	s.mux.HandleFunc("/api/seasonal", s.handleAPISeasonal)
	s.mux.HandleFunc("/api/schedule", s.handleAPISchedule)
	s.mux.HandleFunc("/api/streams/", s.handleAPIStreams)
	s.mux.HandleFunc("/api/player/", s.handleAPIPlayer)
}

// ---- Web pages ----

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	entries, err := s.svc.ListEntries(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if statusFilter != "" {
		filtered := make([]core.UserEntry, 0)
		for _, e := range entries {
			if string(e.Status) == statusFilter {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	type EntryView struct {
		core.UserEntry
		Anime *core.AnimeRecord
	}
	views := make([]EntryView, 0, len(entries))
	for _, e := range entries {
		a, err := s.svc.GetAnimeMeta(r.Context(), e.AnimeID)
		if err != nil {
			continue
		}
		views = append(views, EntryView{UserEntry: e, Anime: a})
	}

	stats, _ := s.svc.Stats(r.Context())

	data := map[string]interface{}{
		"Entries":   views,
		"Statuses":  core.AllStatuses,
		"Current":   statusFilter,
		"Stats":     stats,
		"ActiveTab": "library",
	}
	s.render(w, r, "index.html", data)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	results := make([]*core.AnimeRecord, 0)

	if query != "" {
		var err error
		results, err = s.svc.SearchAnime(r.Context(), query, 20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	data := map[string]interface{}{
		"Query":     query,
		"Results":   results,
		"ActiveTab": "search",
	}
	s.render(w, r, "search.html", data)
}

func (s *Server) handleAnimeDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/anime/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	a, err := s.svc.GetAnime(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	entry, _ := s.svc.GetEntry(r.Context(), id)

	// Find display ID for stream lookup.
	displayID := a.DisplayID()

	data := map[string]interface{}{
		"Anime":        a,
		"Entry":        entry,
		"Statuses":     core.AllStatuses,
		"StreamNames":  s.streamNames,
		"DisplayID":    displayID,
	}
	s.render(w, r, "detail.html", data)
}

func (s *Server) handleSeasonal(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	year := now.Year()
	season := seasonForMonth(now.Month())

	results, err := s.svc.GetSeasonal(r.Context(), year, season)
	if err != nil {
		results = []*core.AnimeRecord{}
	}

	data := map[string]interface{}{
		"Season":    fmt.Sprintf("%s %d", strings.Title(season), year),
		"Anime":     results,
		"ActiveTab": "seasonal",
	}
	s.render(w, r, "seasonal.html", data)
}

func (s *Server) handleTop(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	year := now.Year()
	season := seasonForMonth(now.Month())

	results, err := s.svc.GetSeasonal(r.Context(), year, season)
	if err != nil {
		results = []*core.AnimeRecord{}
	}

	// Filter to airing only, limit to 20.
	airing := make([]*core.AnimeRecord, 0, 20)
	for _, a := range results {
		if a.AirStatus == core.AirStatusAiring {
			airing = append(airing, a)
		}
		if len(airing) >= 20 {
			break
		}
	}
	if len(airing) == 0 && len(results) > 0 {
		if len(results) > 20 {
			airing = results[:20]
		} else {
			airing = results
		}
	}

	data := map[string]interface{}{
		"Season":    fmt.Sprintf("%s %d", strings.Title(season), year),
		"Anime":     airing,
		"ActiveTab": "top",
	}
	s.render(w, r, "top.html", data)
}

func (s *Server) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	// Library stats.
	stats, err := s.svc.Stats(r.Context())
	if err != nil {
		stats = make(map[core.Status]int)
	}
	totalEntries := 0
	for _, v := range stats {
		totalEntries += v
	}

	// History stats (monthly activity).
	type monthStat struct {
		Month string `json:"month"`
		Count int    `json:"count"`
	}
	var monthly []monthStat
	if s.historyPath != "" {
		reader := history.NewReader(s.historyPath)
		entries, err := reader.ReadAll()
		if err == nil {
			monthMap := make(map[string]int)
			for _, e := range entries {
				key := e.Time.Format("2006-01")
				monthMap[key]++
			}
			for month, count := range monthMap {
				monthly = append(monthly, monthStat{Month: month, Count: count})
			}
			sort.Slice(monthly, func(i, j int) bool {
				return monthly[i].Month < monthly[j].Month
			})
			// Keep last 12 months.
			if len(monthly) > 12 {
				monthly = monthly[len(monthly)-12:]
			}
		}
	}

	// Find max count for bar scaling.
	maxCount := 0
	for _, m := range monthly {
		if m.Count > maxCount {
			maxCount = m.Count
		}
	}

	type statRow struct {
		Status core.Status
		Count  int
		Pct    float64
	}
	rows := make([]statRow, 0, len(core.AllStatuses))
	for _, s := range core.AllStatuses {
		count := stats[s]
		pct := 0.0
		if totalEntries > 0 {
			pct = float64(count) / float64(totalEntries) * 100.0
		}
		rows = append(rows, statRow{Status: s, Count: count, Pct: pct})
	}

	data := map[string]interface{}{
		"ActiveTab":   "stats",
		"Total":       totalEntries,
		"Stats":       rows,
		"Monthly":     monthly,
		"MaxCount":    maxCount,
	}
	s.render(w, r, "stats.html", data)
}

// ---- API endpoints ----

func (s *Server) handleAPIAnime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	all, err := s.svc.AllAnimeMeta(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, all)
}

func (s *Server) handleAPIAnimeByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/anime/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a, err := s.svc.GetAnimeMeta(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, a)

	case http.MethodPost:
		var a core.AnimeRecord
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.svc.AddAnimeMeta(r.Context(), &a); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, a)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statusStr := r.URL.Query().Get("status")
	var filters []core.Status
	if statusStr != "" {
		filters = append(filters, core.Status(statusStr))
	}

	entries, err := s.svc.ListEntries(r.Context(), filters...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleAPIEntryByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/entries/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		entry, err := s.svc.GetEntry(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, entry)

	case http.MethodPut:
		var req struct {
			Status          core.Status `json:"status"`
			Score           int         `json:"score"`
			EpisodesWatched int         `json:"episodes_watched"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		entry, err := s.svc.SetStatus(r.Context(), id, req.Status, req.Score, req.EpisodesWatched)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, entry)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query required"})
		return
	}

	results, err := s.svc.SearchAnime(r.Context(), query, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []*core.AnimeRecord{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAPISeasonal(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	year := now.Year()
	season := seasonForMonth(now.Month())

	results, err := s.svc.GetSeasonal(r.Context(), year, season)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleAPISchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := s.svc.GetSchedule(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []*core.ScheduleEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleAPIStreams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /api/streams/{providerRef}
	ref := strings.TrimPrefix(r.URL.Path, "/api/streams/")
	if ref == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "anime id required"})
		return
	}

	// Query all registered stream providers.
	type streamResult struct {
		Provider string              `json:"provider"`
		Sources  []*stream.EpisodeSource `json:"sources,omitempty"`
		Error    string              `json:"error,omitempty"`
	}
	results := make([]streamResult, 0)

	for _, name := range s.streamNames {
		p := stream.Get(name)
		if p == nil {
			continue
		}
		sources, err := p.SearchStreams(r.Context(), ref, 12)
		if err != nil {
			results = append(results, streamResult{
				Provider: name,
				Error:    err.Error(),
			})
			continue
		}
		if len(sources) == 0 {
			continue
		}
		results = append(results, streamResult{
			Provider: name,
			Sources:  sources,
		})
	}

	if results == nil {
		results = []streamResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleAPIPlayer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /api/player/{providerRef}
	ref := strings.TrimPrefix(r.URL.Path, "/api/player/")
	if ref == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "anime id required"})
		return
	}

	episode := 1
	if epStr := r.URL.Query().Get("episode"); epStr != "" {
		fmt.Sscanf(epStr, "%d", &episode)
	}
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		providerName = "default-embed"
	}

	p := stream.Get(providerName)
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "stream provider not found: " + providerName})
		return
	}

	// Get sources for this anime (fetch enough episodes).
	sources, err := p.SearchStreams(r.Context(), ref, episode)
	if err != nil || len(sources) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no streams available"})
		return
	}

	// Find the matching episode.
	var source *stream.EpisodeSource
	for _, s := range sources {
		if s.Episode == episode {
			source = s
			break
		}
	}
	if source == nil {
		source = sources[0]
	}

	url, err := p.GetPlayerURL(r.Context(), source)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": url, "label": source.Label})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func seasonForMonth(m time.Month) string {
	switch m {
	case time.January, time.February, time.March:
		return "winter"
	case time.April, time.May, time.June:
		return "spring"
	case time.July, time.August, time.September:
		return "summer"
	default:
		return "fall"
	}
}
