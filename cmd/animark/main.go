// animark — self-hosted, git-synced anime tracker.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/xvantz/animark/internal/api"
	"github.com/xvantz/animark/internal/core"
	"github.com/xvantz/animark/internal/history"
	"github.com/xvantz/animark/internal/notify"
	"github.com/xvantz/animark/internal/provider"
	"github.com/xvantz/animark/internal/provider/anilist"
	"github.com/xvantz/animark/internal/provider/shikimori"
	"github.com/xvantz/animark/internal/scheduler"
	"github.com/xvantz/animark/internal/service"
	"github.com/xvantz/animark/internal/stream"
)

var version = "0.3.0"

// seasonForMonth mirrors api.seasonForMonth for CLI commands.
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

func main() {
	provider.Register(shikimori.NewClient())
	provider.Register(anilist.NewClient())
	// Register English adapter — wraps AniList, guarantees English titles.
	provider.Register(provider.NewEnglishAdapter(anilist.NewClient()))

	// Register built-in stream adapters (safe — no third-party content).
	// External adapters register themselves in their init().
	stream.Register(stream.NewEmbedProvider("default-embed", "https://example.com/embed/{id}/{episode}", "Default Player", "en"))

	if len(os.Args) < 2 {
		runServer()
		return
	}

	cmd := os.Args[1]
	switch cmd {
	case "version":
		fmt.Println("animark", version)
	case "server", "":
		runServer()
	case "search":
		runSearch()
	case "add":
		runAdd()
	case "list":
		runList()
	case "watch":
		runWatch()
	case "status":
		runStatus()
	case "stats":
		runStats()
	case "import":
		runImport()
	case "schedule":
		runSchedule()
	case "hot", "trending":
		runHot()
	case "play":
		runPlay()
	default:
		if strings.HasPrefix(cmd, "-") {
			runServer()
			return
		}
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "animark — self-hosted, git-synced anime tracker\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  animark [flags]          Start web UI\n")
	fmt.Fprintf(os.Stderr, "  animark version          Print version\n")
	fmt.Fprintf(os.Stderr, "  animark search <q>       Search anime\n")
	fmt.Fprintf(os.Stderr, "  animark add <id>         Add anime to library\n")
	fmt.Fprintf(os.Stderr, "  animark list [status]    List entries\n")
	fmt.Fprintf(os.Stderr, "  animark watch <id> [ep]  Set watching\n")
	fmt.Fprintf(os.Stderr, "  animark status <id> <s>  Change status\n")
	fmt.Fprintf(os.Stderr, "  animark stats            Library stats\n")
	fmt.Fprintf(os.Stderr, "  animark schedule         Today's schedule\n")
	fmt.Fprintf(os.Stderr, "  animark hot              Top 10 seasonal airing\n")
	fmt.Fprintf(os.Stderr, "  animark play <id> [ep]   Open player in browser\n")
	fmt.Fprintf(os.Stderr, "  animark import <fmt> <file>  Import (mal|anilist)\n")
}

func dataDirFromArgs() string {
	for i := 1; i < len(os.Args)-1; i++ {
		if os.Args[i] == "--data" || os.Args[i] == "-data" {
			return os.Args[i+1]
		}
	}
	return "./data"
}

func openService(dataDir string) *service.Service {
	storePath := dataDir + "/anime.json"
	store := core.NewJSONStore(storePath)
	gitCfg := core.GitConfig{
		RemoteURL:   lookupEnv("ANIMARK_GIT_REMOTE", ""),
		Branch:      lookupEnv("ANIMARK_GIT_BRANCH", "main"),
		AuthorName:  lookupEnv("ANIMARK_GIT_AUTHOR", "animark"),
		AuthorEmail: lookupEnv("ANIMARK_GIT_EMAIL", "animark@local"),
		PushEnabled: lookupEnv("ANIMARK_GIT_PUSH", "") == "true",
	}
	gitSync, err := core.NewGitSync(dataDir, store, gitCfg)
	if err != nil {
		log.Printf("warning: git init: %v", err)
	}
	return service.New(store, gitSync)
}

func runServer() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dataDir := flag.String("data", "./data", "Data directory")
	gitRemote := flag.String("git-remote", "", "Remote git URL")
	gitBranch := flag.String("git-branch", "main", "Git branch")
	gitAuthor := flag.String("git-author", "animark", "Git author")
	gitEmail := flag.String("git-email", "animark@local", "Git email")
	gitPush := flag.Bool("git-push", false, "Auto-push")
	flag.Parse()

	storePath := *dataDir + "/anime.json"
	store := core.NewJSONStore(storePath)

	gitCfg := core.GitConfig{
		RemoteURL:   *gitRemote,
		Branch:      *gitBranch,
		AuthorName:  *gitAuthor,
		AuthorEmail: *gitEmail,
		PushEnabled: *gitPush,
	}
	gitSync, err := core.NewGitSync(*dataDir, store, gitCfg)
	if err != nil {
		log.Printf("warning: git init: %v", err)
	}

	svc := service.New(store, gitSync)

	// Set up history logger.
	historyPath := *dataDir + "/history.jsonl"
	historyLogger, err := history.NewLogger(historyPath)
	if err != nil {
		log.Printf("warning: history: %v", err)
	} else {
		svc.WithHistory(historyLogger)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Set up scheduler and notifiers.
	var notifiers []notify.Notifier
	if lookupEnv("ANIMARK_NOTIFY_LOG", "true") == "true" {
		notifiers = append(notifiers, notify.NewLogNotifier())
	}
	if url := lookupEnv("ANIMARK_NOTIFY_DISCORD_URL", ""); url != "" {
		notifiers = append(notifiers, notify.NewDiscordWebhookNotifier(url))
	}
	if url := lookupEnv("ANIMARK_NOTIFY_WEBHOOK_URL", ""); url != "" {
		notifiers = append(notifiers, notify.NewWebhookNotifier("webhook", url))
	}

	sched := scheduler.New(store, notifiers)
	if interval := lookupEnv("ANIMARK_SCHEDULER_INTERVAL", ""); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			sched.WithInterval(d)
		}
	}
	sched.Start(ctx)

	srv, err := api.NewServer(svc)
	if err != nil {
		log.Fatalf("server init: %v", err)
	}
	srv.WithHistoryPath(historyPath)
	srv.WithStreams(stream.List())

	httpServer := &http.Server{Addr: *addr, Handler: srv}

	go func() {
		log.Printf("animark v%s — listening on %s", version, *addr)
		if *gitRemote != "" {
			log.Printf("git-sync → %s (branch: %s, push: %v)", *gitRemote, *gitBranch, *gitPush)
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	httpServer.Shutdown(context.Background())
}

func displayTitle(a *core.AnimeRecord) string {
	if t := a.Title("ru"); t != "Unknown" {
		return t
	}
	if t := a.Title("en"); t != "Unknown" {
		return t
	}
	return a.Title("ja")
}

func runSearch() {
	svc := openService(dataDirFromArgs())
	args := os.Args[2:]
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "Usage: animark search <query> [--data dir]")
		os.Exit(1)
	}
	query := args[0]
	results, err := svc.SearchAnime(context.Background(), query, 15)
	if err != nil {
		log.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		fmt.Println("No results.")
		return
	}
	fmt.Printf("%-36s %-24s %-8s %-5s\n", "Title", "ID", "Status", "Year")
	seen := map[string]bool{}
	for _, a := range results {
		displayID := ""
		if len(a.ProviderRefs) > 0 {
			displayID = a.ProviderRefs[0].CompositeID()
		}
		if seen[displayID] {
			continue
		}
		seen[displayID] = true
		title := displayTitle(a)
		if len(title) > 36 {
			title = title[:33] + "..."
		}
		if len(displayID) > 24 {
			displayID = displayID[:21] + "..."
		}
		fmt.Printf("%-36s %-24s %-8s %-5d\n", title, displayID, a.AirStatus, a.Year)
	}
	// Show all provider IDs for the first result
	if len(results) > 0 {
		first := results[0]
		if len(first.ProviderRefs) > 1 {
			fmt.Printf("\n[info] %s also available as: ", displayTitle(first))
			for _, ref := range first.ProviderRefs {
				fmt.Printf("%s ", ref.CompositeID())
			}
			fmt.Println()
		}
	}
}

func runAdd() {
	svc := openService(dataDirFromArgs())
	args := os.Args[2:]
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: animark add <anime-id> [--data dir]")
		os.Exit(1)
	}
	animeID := args[0]
	ctx := context.Background()

	a, err := svc.GetAnime(ctx, animeID)
	if err != nil {
		log.Fatalf("get anime %s: %v", animeID, err)
	}
	if err := svc.AddAnimeMeta(ctx, a); err != nil {
		log.Fatalf("add metadata: %v", err)
	}
	entry, err := svc.SetStatus(ctx, animeID, core.StatusPlanned, 0, 0)
	if err != nil {
		log.Fatalf("set status: %v", err)
	}
	title := displayTitle(a)
	fmt.Printf("Added: %s (%s) → %s\n", title, animeID, entry.Status)
}

func runList() {
	svc := openService(dataDirFromArgs())
	ctx := context.Background()

	var filter []core.Status
	rest := os.Args[2:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		filter = append(filter, core.Status(rest[0]))
	}

	entries, err := svc.ListEntries(ctx, filter...)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	if len(entries) == 0 {
		fmt.Println("No entries. Use `animark add <anime-id>` to start.")
		return
	}
	fmt.Printf("%-30s %-36s %-14s %3s\n", "Title", "ID (UUID)", "Status", "Ep")
	for _, e := range entries {
		a, err := svc.GetAnimeMeta(ctx, e.AnimeID)
		title := e.AnimeID[:8] + "..."
		displayID := e.AnimeID
		if err == nil {
			title = displayTitle(a)
			displayID = a.DisplayID()
		}
		if len(title) > 30 {
			title = title[:27] + "..."
		}
		total := "?"
		if err == nil && a.EpisodesTotal > 0 {
			total = fmt.Sprintf("%d", a.EpisodesTotal)
		}
		fmt.Printf("%-30s %-36s %-14s %d/%s\n", title, displayID, e.Status, e.EpisodesWatched, total)
	}
}

func runWatch() {
	svc := openService(dataDirFromArgs())
	args := os.Args[2:]
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: animark watch <anime-id> [episode] [--data dir]")
		os.Exit(1)
	}
	animeID := args[0]
	ep := 1
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		if _, err := fmt.Sscanf(args[1], "%d", &ep); err != nil {
			log.Fatalf("invalid episode: %s", args[1])
		}
	}
	entry, err := svc.SetStatus(context.Background(), animeID, core.StatusWatching, 0, ep)
	if err != nil {
		log.Fatalf("watch: %v", err)
	}
	fmt.Printf("Now watching: %s (episode %d)\n", animeID, entry.EpisodesWatched)
}

func runStatus() {
	svc := openService(dataDirFromArgs())
	args := os.Args[2:]
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: animark status <anime-id> <status>")
		fmt.Fprintln(os.Stderr, "Statuses: watching, completed, planned, on_hold, dropped, not_interested, favorite")
		os.Exit(1)
	}
	s := core.Status(args[1])
	if !s.Valid() {
		log.Fatalf("invalid status: %s (valid: %v)", s, core.AllStatuses)
	}
	entry, err := svc.SetStatus(context.Background(), args[0], s, 0, -1)
	if err != nil {
		log.Fatalf("status: %v", err)
	}
	fmt.Printf("%s → %s\n", entry.AnimeID[:8], entry.Status)
}

func runStats() {
	svc := openService(dataDirFromArgs())
	stats, err := svc.Stats(context.Background())
	if err != nil {
		log.Fatalf("stats: %v", err)
	}
	fmt.Println("Library stats:")
	total := 0
	for _, s := range core.AllStatuses {
		total += stats[s]
	}
	for _, s := range core.AllStatuses {
		count := stats[s]
		pct := 0.0
		if total > 0 {
			pct = float64(count) / float64(total) * 100
		}
		fmt.Printf("  %-16s %3d  (%5.1f%%)\n", s, count, pct)
	}
	fmt.Printf("  %-16s %3d\n", "total", total)
}

func runImport() {
	svc := openService(dataDirFromArgs())
	args := os.Args[2:]
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: animark import <format> <file> [--data dir]")
		fmt.Fprintln(os.Stderr, "Formats: mal (MAL XML), anilist (AniList JSON)")
		os.Exit(1)
	}
	format, file := args[0], args[1]
	ctx := context.Background()
	var result *core.ImportResult
	var err error
	switch format {
	case "mal":
		result, err = svc.ImportMAL(ctx, file)
	case "anilist":
		result, err = svc.ImportAniList(ctx, file)
	default:
		log.Fatalf("unknown format %q (use: mal, anilist)", format)
	}
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	fmt.Printf("Import complete: %d total, %d imported, %d skipped, %d errors\n",
		result.Total, result.Imported, result.Skipped, result.Errors)
}

func runSchedule() {
	svc := openService(dataDirFromArgs())
	ctx := context.Background()
	entries, err := svc.GetSchedule(ctx)
	if err != nil {
		log.Fatalf("schedule: %v", err)
	}
	if len(entries) == 0 {
		fmt.Println("No upcoming episodes found.")
		return
	}
	now := time.Now()
	today := now.Weekday().String()
	fmt.Printf("Today's schedule (%s):\n", now.Format("2006-01-02"))
	for _, e := range entries {
		day := e.DayOfWeek
		if day == "" || day == today {
			fmt.Printf("  [ep %d] %s airs at %s\n", e.Episode, e.AnimeID, e.AirTime)
		}
	}
}

func runHot() {
	svc := openService(dataDirFromArgs())
	ctx := context.Background()
	now := time.Now()
	year := now.Year()
	season := seasonForMonth(now.Month())

	results, err := svc.GetSeasonal(ctx, year, season)
	if err != nil {
		log.Fatalf("seasonal: %v", err)
	}

	// Filter to airing only and limit to 10.
	airing := make([]*core.AnimeRecord, 0, 10)
	for _, a := range results {
		if a.AirStatus == core.AirStatusAiring {
			airing = append(airing, a)
		}
		if len(airing) >= 10 {
			break
		}
	}

	if len(airing) == 0 {
		airing = results
		if len(airing) > 10 {
			airing = airing[:10]
		}
	}

	fmt.Printf("Top %d %s %d — Hot Airing Anime:\n\n", len(airing), strings.Title(season), year)
	fmt.Printf("%-3s %-6s %-40s %-24s %4s\n", "#", "Score", "Title", "ID", "Ep")
	for i, a := range airing {
		title := displayTitle(a)
		displayID := a.DisplayID()
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		if len(displayID) > 24 {
			displayID = displayID[:21] + "..."
		}
		eps := fmt.Sprintf("%d/%d", a.EpisodesAired, a.EpisodesTotal)
		if a.EpisodesTotal == 0 {
			eps = fmt.Sprintf("%d/?", a.EpisodesAired)
		}
		fmt.Printf("%-3d %-6.2f %-40s %-24s %4s\n", i+1, a.Score, title, displayID, eps)
	}
	fmt.Println()
	fmt.Println("Add one: animark add <ID>")
}

func runPlay() {
	svc := openService(dataDirFromArgs())
	args := os.Args[2:]
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: animark play <anime-id> [episode] [--data dir]")
		fmt.Fprintln(os.Stderr, "Opens the player in your browser.")
		os.Exit(1)
	}

	animeID := args[0]
	episode := 1
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		if _, err := fmt.Sscanf(args[1], "%d", &episode); err != nil {
			log.Fatalf("invalid episode: %s", args[1])
		}
	}

	ctx := context.Background()

	// Get the anime metadata for display.
	a, err := svc.GetAnimeMeta(ctx, animeID)
	if err != nil {
		log.Fatalf("get anime: %v", err)
	}
	title := displayTitle(a)
	displayID := a.DisplayID()

	fmt.Printf("🔍 Looking for streams: %s (ep %d)\n", title, episode)

	// Query all registered stream providers.
	providerNames := stream.List()
	if len(providerNames) == 0 {
		fmt.Println("No stream providers registered.")
		fmt.Println("Configure at least one stream adapter (see docs/streaming.md).")
		os.Exit(1)
	}

	var found bool
	for _, name := range providerNames {
		p := stream.Get(name)
		if p == nil {
			continue
		}
		sources, err := p.SearchStreams(ctx, displayID, episode)
		if err != nil || len(sources) == 0 {
			continue
		}

		// Find matching episode.
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

		url, err := p.GetPlayerURL(ctx, source)
		if err != nil {
			fmt.Printf("  %s: error: %v\n", name, err)
			continue
		}

		found = true
		fmt.Printf("  %s ▶ %s\n", name, url)

		// Open in browser.
		if err := openBrowser(url); err != nil {
			fmt.Printf("  Could not open browser: %v\n", err)
			fmt.Printf("  Open URL manually: %s\n", url)
		} else {
			fmt.Println("  ✓ Browser opened.")
		}
		break // Use the first working provider.
	}

	if !found {
		fmt.Println("No streams available for this anime.")
		os.Exit(1)
	}
}

// openBrowser opens a URL in the default system browser.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default: // linux
		// Try xdg-open first, then sensible-browser, then gnome-open.
		for _, browser := range []string{"xdg-open", "sensible-browser", "gnome-open"} {
			if _, err := exec.LookPath(browser); err == nil {
				cmd = browser
				args = []string{url}
				break
			}
		}
		if cmd == "" {
			return fmt.Errorf("no browser opener found (install xdg-open)")
		}
	}
	return exec.Command(cmd, args...).Start()
}

func lookupEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
