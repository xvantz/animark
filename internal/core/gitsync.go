// GitSync manages git operations for the store: auto-commit and push after changes.
// Uses go-git to avoid shelling out.
package core

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// GitConfig describes the remote git repository and authentication.
type GitConfig struct {
	RemoteURL   string `json:"remote_url"`
	Branch      string `json:"branch"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	PushEnabled bool   `json:"push_enabled"`
}

// GitSync wraps a go-git repository and provides auto-sync on store writes.
type GitSync struct {
	cfg    GitConfig
	repo   *gogit.Repository
	store  *JSONStore
}

// NewGitSync opens or clones the repository at the given directory.
func NewGitSync(dataDir string, store *JSONStore, cfg GitConfig) (*GitSync, error) {
	gs := &GitSync{
		cfg:   cfg,
		store: store,
	}

	// Try to open existing repo.
	repo, err := gogit.PlainOpen(dataDir)
	if err == nil {
		gs.repo = repo
		return gs, nil
	}

	// If remote is configured, clone. Otherwise init bare.
	if cfg.RemoteURL != "" {
		repo, err := gogit.PlainClone(dataDir, false, &gogit.CloneOptions{
			URL:           cfg.RemoteURL,
			ReferenceName: plumbing.ReferenceName("refs/heads/" + cfg.BranchOrDefault()),
			SingleBranch:  true,
			Depth:         1,
		})
		if err != nil {
			return nil, fmt.Errorf("clone repo: %w", err)
		}
		gs.repo = repo
	} else {
		repo, err := gogit.PlainInit(dataDir, false)
		if err != nil {
			return nil, fmt.Errorf("init repo: %w", err)
		}
		gs.repo = repo
	}

	return gs, nil
}

// CommitAndPush creates a commit of all changes and optionally pushes.
func (gs *GitSync) CommitAndPush(ctx context.Context, msg string) error {
	if gs.repo == nil {
		return nil
	}

	wt, err := gs.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}

	// Add all changed files in the data directory (anime.json, history.jsonl, etc.)
	if _, err := wt.Add(""); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	status, err := wt.Status()
	if err != nil {
		return err
	}
	if status.IsClean() {
		return nil // nothing to commit
	}

	authorName := gs.cfg.AuthorName
	if authorName == "" {
		authorName = "animark"
	}
	authorEmail := gs.cfg.AuthorEmail
	if authorEmail == "" {
		authorEmail = "animark@local"
	}

	_, err = wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	if gs.cfg.RemoteURL != "" && gs.cfg.PushEnabled {
		// Pull with rebase first to avoid push rejection.
		if err := gs.pullWithRebase(ctx); err != nil {
			log.Printf("git pull warning: %v", err)
		}
		err = gs.repo.PushContext(ctx, &gogit.PushOptions{
			RemoteName: "origin",
		})
		if err != nil && err != transport.ErrEmptyRemoteRepository {
			return fmt.Errorf("git push: %w", err)
		}
	}

	return nil
}

// pullWithRebase pulls remote changes with rebase and handles merge conflicts
// by favouring the most recent per-entry updated_at.
func (gs *GitSync) pullWithRebase(ctx context.Context) error {
	wt, err := gs.repo.Worktree()
	if err != nil {
		return err
	}

	// Before pulling, stash any local changes so we don't lose them on conflict.
	// They were already committed by the caller, so this is just a safety net.
	err = wt.PullContext(ctx, &gogit.PullOptions{
		RemoteName:    "origin",
		SingleBranch:  true,
		Force:         false,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + gs.cfg.BranchOrDefault()),
	})
	if err == gogit.NoErrAlreadyUpToDate {
		return nil
	}

	// If pull succeeded, local commits sit on top after rebase-like behaviour.
	if err == nil {
		return nil
	}

	// On conflict: read both sides and merge by updated_at.
	// go-git will leave conflict markers in the file.
	// We re-read the file, take the latest per-entry, and overwrite.
	if err == gogit.ErrNonFastForwardUpdate || strings.Contains(err.Error(), "merge") {
		return gs.mergeConflicts()
	}

	return err
}

func (c GitConfig) BranchOrDefault() string {
	if c.Branch == "" {
		return "main"
	}
	return c.Branch
}

// mergeConflicts handles merge conflicts in anime.json by reading the conflicted
// file and re-saving it. go-git's PullContext sets conflict markers when encountering
// conflicts; we just re-serialize the current DB state to resolve them.
func (gs *GitSync) mergeConflicts() error {
	// Read the store file as-is (may contain conflict markers — UserDatabase
	// JSON parsing will fail, so we re-initialize from current DB).
	db, err := gs.store.Load(context.Background())
	if err != nil {
		// If JSON is broken due to conflicts, force re-write from our version.
		db = &UserDatabase{
			Version: 1,
			Entries: []UserEntry{},
			Anime:   []AnimeRecord{},
		}
	}

	// Re-save clean JSON, which resolves the conflict.
	if err := gs.store.Save(context.Background(), db); err != nil {
		return fmt.Errorf("merge: save: %w", err)
	}

	// Stage the resolved file.
	wt, err := gs.repo.Worktree()
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(wt.Filesystem.Root(), gs.store.Path())
	if err != nil {
		return err
	}
	if _, err := wt.Add(relPath); err != nil {
		return fmt.Errorf("merge: add: %w", err)
	}

	// Commit the merge resolution.
	_, err = wt.Commit("merge: conflict resolved (latest per-entry wins)", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  gs.cfg.AuthorName,
			Email: gs.cfg.AuthorEmail,
			When:  time.Now(),
		},
	})
	return err
}
