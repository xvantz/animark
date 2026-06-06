// Package scheduler periodically checks for new episodes of tracked anime
// and sends notifications through configured notifiers.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xvantz/animark/internal/core"
	"github.com/xvantz/animark/internal/notify"
	"github.com/xvantz/animark/internal/provider"
)

// Scheduler periodically checks for new episodes of tracked anime.
type Scheduler struct {
	store     core.Store
	providers map[string]provider.AnimeProvider
	notifiers []notify.Notifier
	interval  time.Duration
	stopCh    chan struct{}

	// seen tracks the last known episode count per anime to detect changes.
	seen map[string]int
}

// New creates a scheduler. It does not start until Run() is called.
func New(store core.Store, notifiers []notify.Notifier) *Scheduler {
	providers := make(map[string]provider.AnimeProvider)
	for _, name := range provider.List() {
		providers[name] = provider.Get(name)
	}
	return &Scheduler{
		store:     store,
		providers: providers,
		notifiers: notifiers,
		interval:  1 * time.Hour,
		stopCh:    make(chan struct{}),
		seen:      make(map[string]int),
	}
}

// WithInterval changes the check interval (default: 1 hour).
func (s *Scheduler) WithInterval(d time.Duration) *Scheduler {
	s.interval = d
	return s
}

// Start begins the periodic check loop in a background goroutine.
// Returns a function that can be called to stop the scheduler.
func (s *Scheduler) Start(ctx context.Context) func() {
	log.Printf("[scheduler] starting (interval: %s, notifiers: %d)", s.interval, len(s.notifiers))

	// Run immediately on start.
	go func() {
		s.check(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.check(ctx)
			case <-s.stopCh:
				log.Printf("[scheduler] stopped")
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return func() { close(s.stopCh) }
}

// check runs one cycle: load watching entries, check for new episodes, notify.
func (s *Scheduler) check(ctx context.Context) {
	db, err := s.store.Load(ctx)
	if err != nil {
		log.Printf("[scheduler] load: %v", err)
		return
	}

	// Collect watching entries.
	type watchEntry struct {
		entry core.UserEntry
		anime *core.AnimeRecord
	}
	var watching []watchEntry

	for _, e := range db.Entries {
		if e.Status != core.StatusWatching {
			continue
		}
		// Find anime record.
		for i := range db.Anime {
			if db.Anime[i].ID == e.AnimeID {
				watching = append(watching, watchEntry{entry: e, anime: &db.Anime[i]})
				break
			}
		}
	}

	if len(watching) == 0 {
		return
	}

	// For each watching entry, check provider for latest episode count.
	for _, w := range watching {
		s.checkAnime(ctx, w.entry, w.anime)
	}
}

// checkAnime checks one anime for new episodes by querying its providers.
func (s *Scheduler) checkAnime(ctx context.Context, entry core.UserEntry, anime *core.AnimeRecord) {
	if len(anime.ProviderRefs) == 0 {
		return
	}

	ref := anime.ProviderRefs[0]
	p := provider.Get(ref.Provider)
	if p == nil {
		return
	}

	// Fetch current data from provider.
	fresh, err := p.GetByID(ctx, ref.ExternalID)
	if err != nil {
		log.Printf("[scheduler] fetch %s: %v", ref.CompositeID(), err)
		return
	}

	// Check for new episodes.
	lastSeen := s.seen[anime.ID]
	currentAired := fresh.EpisodesAired

	if currentAired <= 0 {
		return
	}

	// First time seeing this anime — just record the count, don't notify.
	if lastSeen == 0 {
		s.seen[anime.ID] = currentAired
		return
	}

	// If aired count increased from last check, notify.
	if currentAired > lastSeen {
		for ep := lastSeen + 1; ep <= currentAired; ep++ {
			s.notify(ctx, anime, ep)
		}
		s.seen[anime.ID] = currentAired
	}
}

// notify sends a notification through all registered notifiers.
func (s *Scheduler) notify(ctx context.Context, anime *core.AnimeRecord, episode int) {
	title := anime.Title("en", "ja", "ru")
	displayID := anime.DisplayID()

	n := &notify.Notification{
		Anime:   anime,
		Episode: episode,
		Title:   title,
		Message: fmt.Sprintf("📺 **%s** — Episode %d is now airing! (%s)",
			title, episode, displayID),
	}

	for _, notif := range s.notifiers {
		if err := notif.Notify(ctx, n); err != nil {
			log.Printf("[scheduler] notify %s: %v", notif.Name(), err)
		}
	}
}

// TrackedCount returns how many anime the scheduler is currently tracking.
func (s *Scheduler) TrackedCount() int {
	return len(s.seen)
}

// ---- CSV history log ----

// HistoryEntry is a single row in the history log.
type HistoryEntry struct {
	Time    time.Time       `json:"time"`
	AnimeID string          `json:"anime_id"`
	Action  string          `json:"action"` // "set_status", "set_episode", "new_episode", "notification"
	OldVal  interface{}     `json:"old_val,omitempty"`
	NewVal  interface{}     `json:"new_val,omitempty"`
}

// FormatCSV returns a CSV line for this entry.
func (e *HistoryEntry) FormatCSV() string {
	oldStr := fmt.Sprintf("%v", e.OldVal)
	newStr := fmt.Sprintf("%v", e.NewVal)
	// Escape commas
	oldStr = strings.ReplaceAll(oldStr, ",", ";")
	newStr = strings.ReplaceAll(newStr, ",", ";")
	return fmt.Sprintf("%s,%s,%s,%s,%s\n",
		e.Time.UTC().Format(time.RFC3339),
		e.AnimeID,
		e.Action,
		oldStr,
		newStr,
	)
}

// HistoryLogger appends history entries to a CSV file.
type HistoryLogger struct {
	store core.Store
}

// NewHistoryLogger creates a history logger that writes to a CSV file
// alongside the main store.
func NewHistoryLogger(store core.Store) *HistoryLogger {
	return &HistoryLogger{store: store}
}

// Log appends a history entry.
func (h *HistoryLogger) Log(entry HistoryEntry) error {
	// For now, log to stderr. In a future iteration, append to a CSV/JSONL file.
	// The format is ready for file output.
	log.Printf("[history] %s | %s | %s | %v → %v",
		entry.Time.Format("2006-01-02 15:04"),
		entry.AnimeID,
		entry.Action,
		entry.OldVal,
		entry.NewVal,
	)
	return nil
}
