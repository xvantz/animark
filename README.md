# animark

**Self-hosted, git-synced anime tracker.** Track what you watch, plan, and love — all stored in a git repository. No cloud, no lock-in.

```
                          ┌──────────────────┐
                          │    Web UI         │
                          │  (htmx + dark)    │
                          └────────┬─────────┘
                                   │
              ┌────────────────────▼────────────────────┐
              │         HTTP Server (Go)                  │
              │  /anime · /search · /entries · /streams  │
              │  /stats · /seasonal · /top · /player     │
              └──────┬──────────┬────────────┬───────────┘
                     │          │             │
              ┌──────▼──┐  ┌───▼────┐  ┌────▼──────┐
              │  Core    │  │ Meta   │  │ Stream    │
              │  JSON    │  │ Prov.  │  │ Prov.     │
              │  + Git   │  │(Shiki) │  │(iframe)   │
              │  + Hist. │  │(AniL.) │  │(BYO)      │
              └─────────┘  └────────┘  └───────────┘
```

## Features

- **Track statuses:** watching, completed, planned, on hold, dropped, not interested, favorite
- **Git-synced:** all data in `anime.json` + `history.jsonl` → auto-commit → push to your remote
- **Provider-based metadata:** Shikimori, AniList, English adapter — provider-independent UUID storage
- **Stream adapter interface:** embed iframe player, BYO (Crunchyroll, AniLibria, etc.)
- **Seasonal + Top:** browse current season, see hot airing anime with ratings
- **Scheduler + notifications:** auto-check new episodes from watching list, notify via log/Discord/webhook
- **History tracking:** JSONL log of all status changes, monthly activity graph
- **CLI + REST API:** full CRUD for integration with anything
- **NixOS module + Docker:** deploy anywhere

## Quick start

### From source

```bash
go install github.com/xvantz/animark/cmd/animark@latest
animark -addr :8080 -data ~/animark-data
```

### With Docker

```bash
docker run -d \
  -p 8080:8080 \
  -v ./animark-data:/data \
  ghcr.io/xvantz/animark
```

### With git remote (auto-sync)

```bash
animark \
  -addr :8080 \
  -data ~/animark-data \
  -git-remote git@github.com:you/animark-data.git \
  -git-push
```

Every status change creates a commit and pushes.

## CLI commands

```bash
animark                    # Start web UI (:8080)
animark version            # Print version
animark search <query>     # Search anime (all providers)
animark add <id>           # Add to library (e.g. shikimori:59970)
animark list [status]      # List entries
animark watch <id> [ep]    # Mark as watching
animark status <id> <s>    # Change status
animark stats              # Library stats
animark schedule           # Today's schedule
animark hot                # Top 10 airing this season
animark play <id> [ep]     # Open player in browser
animark import <fmt> <f>   # Import (mal|anilist)
```

## Web UI

| Page | Route | Description |
|------|-------|-------------|
| Library | `/` | Your entries, filter by status, distribution bars |
| Seasonal | `/seasonal` | Current season catalog |
| 🔥 Top | `/top` | Top 20 airing anime with ratings |
| 📊 Stats | `/stats` | Library stats + monthly activity graph |
| Search | `/search` | Cross-provider search |
| Detail | `/anime/:id` | Card + status + player |

### Built-in player

The detail page includes an **inline video player**. Select episode → provider → play. The embed adapter provides iframe playback; third-party adapters can provide HLS/DASH.

## Notifications (scheduler)

```bash
# Start server with Discord notifications
ANIMARK_NOTIFY_DISCORD_URL="https://discord.com/api/webhooks/..." \
ANIMARK_SCHEDULER_INTERVAL=30m \
animark -addr :8080 -data ./data
```

| Env var | Description |
|---------|-------------|
| `ANIMARK_NOTIFY_LOG` | Log to stdout (default: true) |
| `ANIMARK_NOTIFY_DISCORD_URL` | Discord webhook URL |
| `ANIMARK_NOTIFY_WEBHOOK_URL` | Generic webhook URL |
| `ANIMARK_SCHEDULER_INTERVAL` | Check interval (e.g. `1h`, `30m`) |

## Architecture

```go
// Core entity — provider-independent, UUID-keyed
type AnimeRecord struct {
    ID           string            // UUID (v4)
    Titles       map[string]string // "en", "ru", "ja"
    ProviderRefs []ProviderRef     // [{shikimori,59970}, {anilist,182205}]
    // ...metadata (score, episodes, genres, etc.)
}

// Metadata provider (read-only)
type AnimeProvider interface {
    Name() string
    Search(ctx, query, limit) ([]*AnimeRecord, error)
    GetByID(ctx, id) (*AnimeRecord, error)
    GetSeasonal(ctx, year, season) ([]*AnimeRecord, error)
    GetSchedule(ctx) ([]*ScheduleEntry, error)
}

// Stream provider (video playback)
type StreamProvider interface {
    Name() string
    SearchStreams(ctx, providerRef, limit) ([]*EpisodeSource, error)
    GetStream(ctx, id) (*EpisodeSource, error)
    GetPlayerURL(ctx, source) (string, error)
}
```

## Writing a metadata provider

```go
import "github.com/xvantz/animark/internal/provider"

type MyProvider struct { /* ... */ }

func (p *MyProvider) Name() string              { return "myprovider" }
func (p *MyProvider) Search(ctx, query, limit)   ([]*core.AnimeRecord, error) { /* ... */ }
func (p *MyProvider) GetByID(ctx, id)            (*core.AnimeRecord, error)   { /* ... */ }
func (p *MyProvider) GetSeasonal(ctx, y, s)      ([]*core.AnimeRecord, error) { /* ... */ }
func (p *MyProvider) GetSchedule(ctx)            ([]*core.ScheduleEntry, error) { /* ... */ }

func init() { provider.Register(&MyProvider{}) }
```

## Writing a stream adapter

**Write in a separate Go module** (no copyrighted content in this repo):

```go
package myadapter

import (
    "github.com/xvantz/animark/internal/stream"
)

type MyPlayer struct { /* ... */ }

func (m *MyPlayer) Name() string { return "my-player" }
func (m *MyPlayer) SearchStreams(ctx, ref, limit) ([]*stream.EpisodeSource, error) {
    // Map providerRef (e.g. "shikimori:59970") to your internal ID
    // Return episode sources
}
func (m *MyPlayer) GetStream(ctx, id) (*stream.EpisodeSource, error) { /* ... */ }
func (m *MyPlayer) GetPlayerURL(ctx, src) (string, error) {
    // Return iframe embed URL or HLS manifest URL
}

func init() { stream.Register(&MyPlayer{}) }
```

Use it:
```go
import (
    "github.com/xvantz/animark/internal/stream"
    _ "github.com/yourname/animark-your-adapter"
)
```

## Data format

The user library lives in `data/anime.json` (v2, UUID-based):

```json
{
  "version": 2,
  "updated": "2026-06-06T10:00:00Z",
  "entries": [
    {
      "anime_id": "d4c5b2a1-...",
      "status": "watching",
      "score": 9,
      "episodes_watched": 12
    }
  ],
  "anime": [
    {
      "id": "d4c5b2a1-...",
      "titles": {"en": "Steins;Gate", "ru": "Врата Штейна"},
      "provider_refs": [{"provider": "shikimori", "external_id": "5114"}],
      "episodes_total": 24
    }
  ]
}
```

History is stored in `data/history.jsonl` (JSONL format, append-only).

## REST API

```http
GET  /api/search?q=steins          # Search anime
GET  /api/anime                    # All metadata
GET  /api/anime/:id                # Single anime
POST /api/anime/:id                # Add metadata
GET  /api/entries                  # All user entries (?status=watching)
GET  /api/entries/:id              # Single entry
PUT  /api/entries/:id              # Set status
GET  /api/stats                    # Stats per status
GET  /api/seasonal                 # Current seasonal anime
GET  /api/schedule                 # Today's schedule
GET  /api/streams/:ref             # Available streams
GET  /api/player/:ref?episode=N&provider=NAME  # Player URL
```

## Development

```bash
git clone https://github.com/xvantz/animark
cd animark
nix develop          # NixOS — enter dev shell
go mod download      # Non-NixOS
go run ./cmd/animark -addr :8080 -data ./testdata
```

### Run tests

```bash
go test ./... -count=1 -timeout 60s
```

### Build

```bash
go build -o animark ./cmd/animark
```

## License

MIT
