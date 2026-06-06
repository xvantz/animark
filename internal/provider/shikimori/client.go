// Package shikimori implements the AnimeProvider interface for shikimori.one API.
package shikimori

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xvantz/animark/internal/core"
)

const (
	baseURL   = "https://shikimori.one"
	apiPrefix = "/api"
	userAgent = "animark/0.1 (github.com/xvantz/animark)"
)

// Client is a Shikimori API client implementing AnimeProvider.
type Client struct {
	httpClient *http.Client
	base       string
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		base: baseURL + apiPrefix,
	}
}

func (c *Client) Name() string { return "shikimori" }

// ---- Provider interface ----

func (c *Client) Search(ctx context.Context, query string, limit int) ([]*core.AnimeRecord, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	params := url.Values{
		"search": {query},
		"limit":  {strconv.Itoa(limit)},
	}
	var raw []animeResponse
	if err := c.get(ctx, "/animes?"+params.Encode(), &raw); err != nil {
		return nil, err
	}
	result := make([]*core.AnimeRecord, 0, len(raw))
	for _, r := range raw {
		result = append(result, r.toCore(c.Name()))
	}
	return result, nil
}

func (c *Client) GetByID(ctx context.Context, id string) (*core.AnimeRecord, error) {
	var raw animeResponse
	if err := c.get(ctx, "/animes/"+id, &raw); err != nil {
		return nil, err
	}
	return raw.toCore(c.Name()), nil
}

func (c *Client) GetSeasonal(ctx context.Context, year int, season string) ([]*core.AnimeRecord, error) {
	params := url.Values{
		"season": {fmt.Sprintf("%s_%d", season, year)},
		"limit":  {"50"},
		"order":  {"popularity"},
	}
	var raw []animeResponse
	if err := c.get(ctx, "/animes?"+params.Encode(), &raw); err != nil {
		return nil, err
	}
	result := make([]*core.AnimeRecord, 0, len(raw))
	for _, r := range raw {
		result = append(result, r.toCore(c.Name()))
	}
	return result, nil
}

func (c *Client) GetSchedule(ctx context.Context) ([]*core.ScheduleEntry, error) {
	var raw []animeResponse
	if err := c.get(ctx, "/animes?status=ongoing&limit=50", &raw); err != nil {
		return nil, err
	}
	entries := make([]*core.ScheduleEntry, 0)
	for _, r := range raw {
		if r.AiredOn == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", r.AiredOn)
		if err != nil {
			continue
		}
		entries = append(entries, &core.ScheduleEntry{
			AnimeID:   strconv.Itoa(r.ID),
			Provider:  c.Name(),
			Episode:   0,
			AirTime:   t.Format(time.RFC3339),
			DayOfWeek: t.Weekday().String(),
		})
	}
	return entries, nil
}

// ---- API helpers ----

func (c *Client) get(ctx context.Context, path string, dest interface{}) error {
	reqURL := c.base + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("shikimori request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("shikimori get %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shikimori %s: %s", path, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("shikimori decode: %w", err)
	}
	return nil
}

// ---- Response types ----

type animeResponse struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Russian         string    `json:"russian"`
	Image           imageObj  `json:"image"`
	URL             string    `json:"url"`
	Kind            string    `json:"kind"`
	Score           string    `json:"score"`
	Status          string    `json:"status"`
	Episodes        int       `json:"episodes"`
	EpisodesAired   int       `json:"episodes_aired"`
	AiredOn         string    `json:"aired_on"`
	ReleasedOn      string    `json:"released_on"`
	Description     string    `json:"description"`
	DescriptionHTML string    `json:"description_html"`
	Genres          []genreObj `json:"genres"`
	Japanese        []string  `json:"japanese"`
	Synonyms        []string  `json:"synonyms"`
	Season          string    `json:"season"`
}

type imageObj struct {
	Original string `json:"original"`
	Preview  string `json:"preview"`
	X48      string `json:"x48"`
	X96      string `json:"x96"`
}

type genreObj struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Russian string `json:"russian"`
}

func (r *animeResponse) toCore(provider string) *core.AnimeRecord {
	titles := map[string]string{
		"en": r.Name,
	}
	if r.Russian != "" {
		titles["ru"] = r.Russian
	}
	if len(r.Japanese) > 0 {
		titles["ja"] = strings.Join(r.Japanese, ", ")
	}

	a := &core.AnimeRecord{
		ProviderRefs: []core.ProviderRef{
			{Provider: provider, ExternalID: strconv.Itoa(r.ID)},
		},
		Titles:        titles,
		EpisodesTotal: r.Episodes,
		EpisodesAired: r.EpisodesAired,
		Score:         parseScore(r.Score),
		Season:        r.Season,
		URL:           baseURL + r.URL,
		Description:   cleanDescription(r.Description),
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	if r.Image.Original != "" {
		a.ImageURL = baseURL + r.Image.Original
	} else if r.Image.Preview != "" {
		a.ImageURL = baseURL + r.Image.Preview
	}

	if r.Status == "ongoing" {
		a.AirStatus = core.AirStatusAiring
	} else if r.Status == "released" || r.Status == "anons" {
		a.AirStatus = core.AirStatusFinished
	} else {
		a.AirStatus = core.AirStatusUnknown
	}

	year, _ := parseYear(r.AiredOn)
	a.Year = year

	for _, g := range r.Genres {
		name := g.Name
		if g.Russian != "" {
			name = g.Russian
		}
		a.Genres = append(a.Genres, name)
	}

	return a
}

func parseScore(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseYear(dateStr string) (int, error) {
	if dateStr == "" {
		return 0, nil
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return 0, err
	}
	return t.Year(), nil
}

func cleanDescription(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	return s
}
