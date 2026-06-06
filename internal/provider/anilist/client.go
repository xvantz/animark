// Package anilist implements the AnimeProvider interface for anilist.co GraphQL API.
package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xvantz/animark/internal/core"
)

const (
	apiURL    = "https://graphql.anilist.co"
	userAgent = "animark/0.1 (github.com/xvantz/animark)"
)

// Client is an AniList API client implementing AnimeProvider.
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) Name() string { return "anilist" }

// ---- GraphQL queries ----

const searchQuery = `
query ($search: String, $page: Int, $perPage: Int) {
  Page(page: $page, perPage: $perPage) {
    media(search: $search, type: ANIME, sort: SEARCH_MATCH) {
      id
      title { romaji english native }
      coverImage { large }
      episodes
      status
      season
      seasonYear
      averageScore
      description
      genres
      duration
      format
      startDate { year month day }
      endDate { year month day }
      nextAiringEpisode { episode airingAt }
      siteUrl
    }
  }
}`

const getByIDQuery = `
query ($id: Int) {
  Media(id: $id, type: ANIME) {
    id
    title { romaji english native }
    coverImage { large }
    episodes
    status
    season
    seasonYear
    averageScore
    description
    genres
    duration
    format
    startDate { year month day }
    endDate { year month day }
    nextAiringEpisode { episode airingAt }
    siteUrl
  }
}`

const seasonalQuery = `
query ($season: MediaSeason, $seasonYear: Int, $page: Int, $perPage: Int) {
  Page(page: $page, perPage: $perPage) {
    media(season: $season, seasonYear: $seasonYear, type: ANIME, sort: POPULARITY_DESC) {
      id
      title { romaji english native }
      coverImage { large }
      episodes
      status
      season
      seasonYear
      averageScore
      description
      genres
      startDate { year month day }
      endDate { year month day }
      nextAiringEpisode { episode airingAt }
      siteUrl
    }
  }
}`

const scheduleQuery = `
query ($page: Int, $perPage: Int) {
  Page(page: $page, perPage: $perPage) {
    airingSchedules(notYetAired: true, sort: TIME, perPage: 20) {
      episode
      airingAt
      media {
        id
        title { romaji english native }
        coverImage { large }
        episodes
        status
      }
    }
  }
}`

// ---- Provider interface ----

func (c *Client) Search(ctx context.Context, query string, limit int) ([]*core.AnimeRecord, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	vars := map[string]interface{}{
		"search":  query,
		"page":    1,
		"perPage": limit,
	}
	var resp struct {
		Data struct {
			Page struct {
				Media []mediaResponse `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := c.graphQL(ctx, searchQuery, vars, &resp); err != nil {
		return nil, err
	}
	return convertSlice(resp.Data.Page.Media, c.Name()), nil
}

func (c *Client) GetByID(ctx context.Context, id string) (*core.AnimeRecord, error) {
	var intID int
	if _, err := fmt.Sscanf(id, "%d", &intID); err != nil {
		return nil, fmt.Errorf("invalid anilist id %q", id)
	}
	vars := map[string]interface{}{"id": intID}
	var resp struct {
		Data struct {
			Media mediaResponse `json:"Media"`
		} `json:"data"`
	}
	if err := c.graphQL(ctx, getByIDQuery, vars, &resp); err != nil {
		return nil, err
	}
	if resp.Data.Media.ID == 0 {
		return nil, fmt.Errorf("anilist: anime %d not found", intID)
	}
	return resp.Data.Media.toCore(c.Name()), nil
}

func (c *Client) GetSeasonal(ctx context.Context, year int, season string) ([]*core.AnimeRecord, error) {
	alSeason := toALSeason(season)
	if alSeason == "" {
		return nil, fmt.Errorf("unknown season %q", season)
	}
	vars := map[string]interface{}{
		"season":     alSeason,
		"seasonYear": year,
		"page":       1,
		"perPage":    50,
	}
	var resp struct {
		Data struct {
			Page struct {
				Media []mediaResponse `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := c.graphQL(ctx, seasonalQuery, vars, &resp); err != nil {
		return nil, err
	}
	return convertSlice(resp.Data.Page.Media, c.Name()), nil
}

func (c *Client) GetSchedule(ctx context.Context) ([]*core.ScheduleEntry, error) {
	vars := map[string]interface{}{
		"page":    1,
		"perPage": 20,
	}
	var resp struct {
		Data struct {
			Page struct {
				AiringSchedules []struct {
					Episode  int           `json:"episode"`
					AiringAt int64         `json:"airingAt"`
					Media    mediaResponse `json:"media"`
				} `json:"airingSchedules"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := c.graphQL(ctx, scheduleQuery, vars, &resp); err != nil {
		return nil, err
	}

	entries := make([]*core.ScheduleEntry, 0, len(resp.Data.Page.AiringSchedules))
	for _, s := range resp.Data.Page.AiringSchedules {
		t := time.Unix(s.AiringAt, 0).UTC()
		entries = append(entries, &core.ScheduleEntry{
			AnimeID:   fmt.Sprintf("%s:%d", c.Name(), s.Media.ID),
			Provider:  c.Name(),
			Episode:   s.Episode,
			AirTime:   t.Format(time.RFC3339),
			DayOfWeek: t.Weekday().String(),
		})
	}
	return entries, nil
}

// ---- GraphQL helpers ----

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func (c *Client) graphQL(ctx context.Context, query string, vars map[string]interface{}, dest interface{}) error {
	body := graphQLRequest{Query: query, Variables: vars}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return fmt.Errorf("anilist encode: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &buf)
	if err != nil {
		return fmt.Errorf("anilist request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("anilist post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if len(errResp.Errors) > 0 {
			return fmt.Errorf("anilist error: %s", errResp.Errors[0].Message)
		}
		return fmt.Errorf("anilist: %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("anilist decode: %w", err)
	}
	return nil
}

// ---- Response types ----

type mediaResponse struct {
	ID        int       `json:"id"`
	Title     titleObj  `json:"title"`
	CoverImage struct {
		Large string `json:"large"`
	} `json:"coverImage"`
	Episodes          int        `json:"episodes"`
	Status            string     `json:"status"`
	Season            string     `json:"season"`
	SeasonYear        int        `json:"seasonYear"`
	AverageScore      int        `json:"averageScore"`
	Description       string     `json:"description"`
	Genres            []string   `json:"genres"`
	Duration          int        `json:"duration"`
	Format            string     `json:"format"`
	StartDate         dateObj    `json:"startDate"`
	EndDate           dateObj    `json:"endDate"`
	NextAiringEpisode *airingObj `json:"nextAiringEpisode"`
	SiteURL           string     `json:"siteUrl"`
}

type titleObj struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

type dateObj struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type airingObj struct {
	Episode  int   `json:"episode"`
	AiringAt int64 `json:"airingAt"`
}

// ---- Conversion ----

func (m mediaResponse) toCore(provider string) *core.AnimeRecord {
	titles := map[string]string{}
	if m.Title.English != "" {
		titles["en"] = m.Title.English
	}
	if m.Title.Romaji != "" {
		titles["ja"] = m.Title.Romaji
	}
	if m.Title.Native != "" {
		titles["native"] = m.Title.Native
	}

	ref := core.ProviderRef{
		Provider:   provider,
		ExternalID: fmt.Sprintf("%d", m.ID),
	}

	a := &core.AnimeRecord{
		ProviderRefs:  []core.ProviderRef{ref},
		Titles:        titles,
		ImageURL:      m.CoverImage.Large,
		EpisodesTotal: m.Episodes,
		Score:         float64(m.AverageScore) / 10.0,
		Season:        seasonString(m.Season, m.SeasonYear),
		Year:          m.SeasonYear,
		URL:           m.SiteURL,
		Genres:        m.Genres,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	if m.Description != "" {
		a.Description = stripHTML(m.Description)
	}

	switch m.Status {
	case "RELEASING":
		a.AirStatus = core.AirStatusAiring
		if m.NextAiringEpisode != nil {
			a.EpisodesAired = m.NextAiringEpisode.Episode - 1
		}
	case "FINISHED":
		a.AirStatus = core.AirStatusFinished
		a.EpisodesAired = m.Episodes
	case "NOT_YET_RELEASED":
		a.AirStatus = core.AirStatusUpcoming
	default:
		a.AirStatus = core.AirStatusUnknown
	}

	return a
}

func convertSlice(items []mediaResponse, provider string) []*core.AnimeRecord {
	result := make([]*core.AnimeRecord, 0, len(items))
	for _, m := range items {
		if m.ID == 0 {
			continue
		}
		result = append(result, m.toCore(provider))
	}
	return result
}

func seasonString(season string, year int) string {
	if season == "" || year == 0 {
		return ""
	}
	return fmt.Sprintf("%s-%d", season, year)
}

func toALSeason(s string) string {
	switch s {
	case "winter":
		return "WINTER"
	case "spring":
		return "SPRING"
	case "summer":
		return "SUMMER"
	case "fall":
		return "FALL"
	default:
		return ""
	}
}

func stripHTML(s string) string {
	var result []byte
	inTag := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
			continue
		}
		if s[i] == '>' {
			inTag = false
			result = append(result, ' ')
			continue
		}
		if !inTag {
			result = append(result, s[i])
		}
	}
	return string(result)
}
