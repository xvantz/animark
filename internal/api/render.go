package api

import (
	"bytes"
	"html/template"
	"log"
	"net/http"

	"github.com/xvantz/animark/internal/core"
)

// StatEntry represents a single status with count and percentage.
type StatEntry struct {
	Status  core.Status
	Count   int
	Percent float64
}

// templateFuncs provides safe accessors for AnimeRecord fields in templates.
var templateFuncs = template.FuncMap{
	"title": func(a *core.AnimeRecord, langs ...string) string {
		if a == nil {
			return "Unknown"
		}
		for _, lang := range langs {
			if t, ok := a.Titles[lang]; ok && t != "" {
				return t
			}
		}
		for _, lang := range []string{"ru", "en", "ja"} {
			if t, ok := a.Titles[lang]; ok && t != "" {
				return t
			}
		}
		return "Unknown"
	},
	"displayID": func(a *core.AnimeRecord) string {
		if a == nil {
			return ""
		}
		return a.DisplayID()
	},
	"safeTitle": func(titles map[string]string, langs ...string) string {
		if titles == nil {
			return "Unknown"
		}
		for _, lang := range langs {
			if t, ok := titles[lang]; ok && t != "" {
				return t
			}
		}
		for _, lang := range []string{"ru", "en", "ja"} {
			if t, ok := titles[lang]; ok && t != "" {
				return t
			}
		}
		return "Unknown"
	},
	"divf": func(a, b float64) float64 {
		if b == 0 {
			return 0
		}
		return a / b
	},
	"mulf": func(a, b float64) float64 {
		return a * b
	},
	"seq": func(start, end int) []int {
		if start > end {
			return nil
		}
		s := make([]int, 0, end-start+1)
		for i := start; i <= end; i++ {
			s = append(s, i)
		}
		return s
	},
}

// render wraps page content in the layout template.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	content := pageTemplates[name]
	if content == "" {
		content = "<p>Template not found: " + name + "</p>"
	}

	// Parse page content with the same FuncMap.
	pageTmpl, err := template.New(name).Funcs(templateFuncs).Parse(content)
	if err != nil {
		log.Printf("page template parse error %s: %v", name, err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, data); err != nil {
		log.Printf("page render error %s: %v", name, err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}

	// Pre-compute stats with percentages for the template.
	rawStats, _ := data["Stats"].(map[core.Status]int)
	statsWithPct := make([]StatEntry, 0, len(core.AllStatuses))
	totalEntries := 0
	for _, v := range rawStats {
		totalEntries += v
	}
	for _, s := range core.AllStatuses {
		count := rawStats[s]
		pct := 0.0
		if totalEntries > 0 {
			pct = float64(count) / float64(totalEntries) * 100.0
		}
		statsWithPct = append(statsWithPct, StatEntry{Status: s, Count: count, Percent: pct})
	}
	data["StatsWithPct"] = statsWithPct

	layoutData := map[string]interface{}{
		"Content":   template.HTML(buf.String()),
		"ActiveTab": data["ActiveTab"],
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.layout.ExecuteTemplate(w, "base.html", layoutData); err != nil {
		log.Printf("layout render error: %v", err)
	}
}

// pageTemplates holds standalone HTML fragments, one per page.
// Each is a complete <div> or content section, rendered then injected into the layout.
var pageTemplates = map[string]string{
	// ── Index / Library ──
	"index.html": `
<div class="stats-bar">
    {{range $s := .Statuses}}
    <span><strong>{{index $.Stats $s}}</strong> {{$s}}</span>
    {{end}}
</div>

<h3 style="margin-top:24px;font-size:1rem;color:var(--text2)">Distribution</h3>
<div class="stat-chart">
    {{range $se := .StatsWithPct}}
    <div class="stat-row">
        <span class="stat-label">{{$se.Status}}</span>
        <div class="stat-bar-bg">
            <div class="stat-bar-fill status-{{$se.Status}}" style="width:{{printf "%.0f" $se.Percent}}%"></div>
        </div>
        <span class="stat-count">{{$se.Count}}</span>
    </div>
    {{end}}
</div>

<div class="filters">
    <a href="/" {{if not .Current}}class="active"{{end}}>All</a>
    {{range .Statuses}}
    <a href="/?status={{.}}" {{if eq (print .) $.Current}}class="active"{{end}}>{{.}}</a>
    {{end}}
</div>

<div class="grid">
    {{range .Entries}}
    <a href="/anime/{{.Anime.ID}}" class="card" style="text-decoration:none;color:inherit">
        <img src="{{.Anime.ImageURL}}" alt="{{safeTitle .Anime.Titles "en"}}" loading="lazy" onerror="this.src='data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 200 267%22><rect fill=%22252535%22 width=%22200%22 height=%22267%22/><text x=%2250%%22 y=%2250%%22 text-anchor=%22middle%22 fill=%228888aa%22 font-size=%2214%22>No Image</text></svg>'">
        <div class="info">
            <h3>{{safeTitle .Anime.Titles "ru" "en"}}</h3>
            <div class="sub">{{.Status}} · {{.EpisodesWatched}}/{{.Anime.EpisodesTotal}}</div>
        </div>
    </a>
    {{else}}
    <div style="grid-column:1/-1;text-align:center;padding:60px 0;color:var(--text2)">
        <p>Your library is empty.</p>
        <p style="margin-top:8px"><a href="/search" style="color:var(--accent)">Search and add anime</a> to get started.</p>
    </div>
    {{end}}
</div>
`,

	// ── Search ──
	"search.html": `
<div class="page-heading">Search</div>

<form action="/search" method="GET" style="margin:16px 0;display:flex;gap:8px">
    <input type="text" name="q" value="{{.Query}}" placeholder="Search by title..."
        style="flex:1;background:var(--surface2);border:1px solid var(--border);border-radius:8px;padding:10px 16px;color:var(--text);font-size:1rem;outline:none">
    <button type="submit" style="background:var(--accent);border:none;border-radius:8px;padding:10px 24px;color:white;cursor:pointer;font-weight:600">Search</button>
</form>

<div class="search-results">
    {{if .Query}}
        <p style="color:var(--text2);margin-bottom:12px">Results for "{{.Query}}"</p>
    {{end}}

    <div class="grid">
        {{range .Results}}
        <a href="/anime/{{.ID}}" class="card" style="text-decoration:none;color:inherit">
            <img src="{{.ImageURL}}" alt="{{safeTitle .Titles "en"}}" loading="lazy" onerror="this.src='data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 200 267%22><rect fill=%22252535%22 width=%22200%22 height=%22267%22/><text x=%2250%%22 y=%2250%%22 text-anchor=%22middle%22 fill=%228888aa%22 font-size=%2214%22>No Image</text></svg>'">
            <div class="info">
                <h3>{{safeTitle .Titles "ru" "en"}}</h3>
                <div class="sub">{{.Year}} · {{range $i, $g := .Genres}}{{if $i}}, {{end}}{{$g}}{{end}}</div>
            </div>
        </a>
        {{else}}
            {{if .Query}}
            <div style="grid-column:1/-1;text-align:center;padding:40px;color:var(--text2)">
                No results found. Try a different search term.
            </div>
            {{end}}
        {{end}}
    </div>
</div>
`,

	// ── Detail ──
	"detail.html": `
{{$anime := .Anime}}
{{$entry := .Entry}}
{{$title := safeTitle $anime.Titles "ru" "en" "ja"}}

<div class="detail">
    <div class="poster">
        <img src="{{$anime.ImageURL}}" alt="{{$title}}" onerror="this.src='data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 200 267%22><rect fill=%22252535%22 width=%22200%22 height=%22267%22/><text x=%2250%%22 y=%2250%%22 text-anchor=%22middle%22 fill=%228888aa%22 font-size=%2214%22>No Image</text></svg>'">
    </div>
    <div class="meta">
        <h1>{{$title}}</h1>
        {{$en := index $anime.Titles "en"}}{{$ru := index $anime.Titles "ru"}}{{$ja := index $anime.Titles "ja"}}
        {{if and $ru $en}}<div class="alt-title">{{$en}}</div>{{end}}
        {{if $ja}}<div class="alt-title">{{$ja}}</div>{{end}}

        <div class="tags">
            {{range $anime.Genres}}<span>{{.}}</span>{{end}}
            {{if $anime.Year}}<span>{{$anime.Year}}</span>{{end}}
            <span>{{$anime.AirStatus}}</span>
            <span>{{$anime.EpisodesTotal}} eps</span>
            {{if gt $anime.Score 0.0}}<span>★ {{printf "%.1f" $anime.Score}}</span>{{end}}
        </div>

        {{if $anime.Description}}
        <div class="desc">{{$anime.Description}}</div>
        {{end}}

        <div class="status-select">
            {{range $s := .Statuses}}
            <a href="#"
               class="{{if $entry}}{{if eq (print $entry.Status) (print $s)}}active{{end}}{{end}}"
               hx-put="/api/entries/{{$anime.ID}}"
               hx-headers='{"Content-Type": "application/json"}'
               hx-vals='{"status":"{{$s}}","score":{{if $entry}}{{$entry.Score}}{{else}}0{{end}},"episodes_watched":{{if $entry}}{{$entry.EpisodesWatched}}{{else}}0{{end}}}'
               hx-trigger="click"
               hx-swap="none"
               onclick="window.location.reload()">{{$s}}</a>
            {{end}}
        </div>

        {{if $entry}}
        <div style="margin-top:12px;color:var(--text2);font-size:0.85rem">
            Watched: <strong>{{$entry.EpisodesWatched}}</strong> / {{$anime.EpisodesTotal}} episodes ·
            Score: <strong>{{$entry.Score}}</strong>/10 ·
            {{if not $entry.StartedAt.IsZero}}Started: {{$entry.StartedAt.Format "Jan 2, 2006"}}{{end}}
        </div>
        {{end}}

        {{if .StreamNames}}
        <div style="margin-top:24px">
            <h3 style="font-size:1rem;margin-bottom:12px;color:var(--text);display:flex;align-items:center;gap:8px">
                ▶ Watch
                <span style="font-size:0.75rem;color:var(--text2);font-weight:400">Select episode &amp; provider</span>
            </h3>

            <!-- Episode selector -->
            <div id="episode-selector" style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px">
                {{range $ep := seq 1 12}}
                <button class="ep-btn" data-ep="{{$ep}}"
                    style="background:var(--surface2);border:1px solid var(--border);border-radius:6px;padding:4px 12px;color:var(--text2);cursor:pointer;font-size:0.8rem"
                    onclick="document.querySelectorAll('.ep-btn').forEach(b=>b.style.color='var(--text2)');this.style.color='var(--accent)';this.style.borderColor='var(--accent)';document.getElementById('stream-providers').style.display='flex';window._selectedEp={{$ep}}">{{$ep}}</button>
                {{end}}
            </div>

            <!-- Provider selector (hidden until episode picked) -->
            <div id="stream-providers" style="display:none;gap:8px;flex-wrap:wrap;margin-bottom:12px">
                {{range .StreamNames}}
                <button class="prov-btn" data-provider="{{.}}"
                    style="background:var(--surface);border:1px solid var(--border);border-radius:6px;padding:6px 16px;color:var(--text2);cursor:pointer;font-size:0.85rem"
                    onclick="var ep=window._selectedEp||1;this.closest('#stream-providers').querySelectorAll('.prov-btn').forEach(b=>b.style.borderColor='var(--border)');this.style.borderColor='var(--accent)';var iframe=document.getElementById('player-iframe');if(iframe){var url='/api/player/{{$.DisplayID}}?episode='+ep+'&provider={{.}}';fetch(url).then(r=>r.json()).then(d=>{iframe.src=d.url;iframe.style.display='block';document.getElementById('player-placeholder').style.display='none'})}">{{.}}</button>
                {{end}}
            </div>

            <!-- Player -->
            <div id="player-container" style="background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);overflow:hidden;aspect-ratio:16/9;position:relative">
                <div id="player-placeholder" style="display:flex;align-items:center;justify-content:center;height:100%;color:var(--text2);font-size:0.9rem;text-align:center;padding:20px">
                    <div>Select episode and provider to start watching</div>
                </div>
                <iframe id="player-iframe"
                    style="display:none;width:100%;height:100%;border:none"
                    allowfullscreen
                    sandbox="allow-scripts allow-same-origin allow-popups allow-forms">
                </iframe>
            </div>
        </div>
        {{end}}
    </div>
</div>
`,

	// ── Seasonal ──
	"seasonal.html": `
<div class="page-heading">Seasonal — {{.Season}}</div>

<div class="grid">
    {{range .Anime}}
    <a href="/anime/{{.ID}}" class="card" style="text-decoration:none;color:inherit">
        <img src="{{.ImageURL}}" alt="{{safeTitle .Titles "en"}}" loading="lazy" onerror="this.src='data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 200 267%22><rect fill=%22252535%22 width=%22200%22 height=%22267%22/><text x=%2250%%22 y=%2250%%22 text-anchor=%22middle%22 fill=%228888aa%22 font-size=%2214%22>No Image</text></svg>'">
        <div class="info">
            <h3>{{safeTitle .Titles "ru" "en"}}</h3>
            <div class="sub">{{.Year}} · ★ {{printf "%.1f" .Score}}</div>
        </div>
    </a>
    {{else}}
    <div style="grid-column:1/-1;text-align:center;padding:60px 0;color:var(--text2)">
        No seasonal data available.
    </div>
    {{end}}
</div>
`,

	// ── Top ──
	"top.html": `
<div class="page-heading">🔥 Top Airing — {{.Season}}</div>

<div class="grid">
    {{range .Anime}}
    <a href="/anime/{{.ID}}" class="card" style="text-decoration:none;color:inherit">
        <img src="{{.ImageURL}}" alt="{{safeTitle .Titles "en"}}" loading="lazy" onerror="this.src='data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 200 267%22><rect fill=%22252535%22 width=%22200%22 height=%22267%22/><text x=%2250%%22 y=%2250%%22 text-anchor=%22middle%22 fill=%228888aa%22 font-size=%2214%22>No Image</text></svg>'">
        <div class="info">
            <h3>{{safeTitle .Titles "ru" "en"}}</h3>
            <div class="sub">★ {{printf "%.2f" .Score}} · {{.EpisodesAired}}/{{if gt .EpisodesTotal 0}}{{.EpisodesTotal}}{{else}}?{{end}} eps</div>
        </div>
    </a>
    {{else}}
    <div style="grid-column:1/-1;text-align:center;padding:60px 0;color:var(--text2)">
        No airing anime found for this season.
    </div>
    {{end}}
</div>
`,

	// ── Stats ──
	"stats.html": `
<div class="page-heading">📊 Statistics</div>

<h3 style="margin-top:20px;font-size:1rem;color:var(--text2)">Library Distribution ({{.Total}} total)</h3>
<div class="stat-chart">
    {{range .Stats}}
    <div class="stat-row">
        <span class="stat-label">{{.Status}}</span>
        <div class="stat-bar-bg">
            <div class="stat-bar-fill" style="width:{{printf "%.0f" .Pct}}%;background:var(--accent);height:10px;border-radius:5px;min-width:0"></div>
        </div>
        <span class="stat-count">{{.Count}}</span>
    </div>
    {{end}}
</div>

{{if .Monthly}}
<h3 style="margin-top:28px;font-size:1rem;color:var(--text2)">Monthly Activity</h3>
<div class="stat-chart">
    {{range .Monthly}}
    <div class="stat-row">
        <span class="stat-label" style="min-width:70px">{{.Month}}</span>
        <div class="stat-bar-bg">
            {{$pct := 0.0}}{{if gt $.MaxCount 0}}{{$pct = divf .Count $.MaxCount | mulf 100}}{{end}}
            <div class="stat-bar-fill" style="width:{{printf "%.0f" $pct}}%;background:var(--green);height:10px;border-radius:5px;min-width:0"></div>
        </div>
        <span class="stat-count">{{.Count}}</span>
    </div>
    {{end}}
</div>
{{else}}
<p style="color:var(--text2);margin-top:24px">No history data yet. Start adding and updating anime to see activity.</p>
{{end}}
`,
}
