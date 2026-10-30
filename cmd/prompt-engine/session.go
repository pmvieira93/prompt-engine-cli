package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/transcript"
)

// How much of an exchange the expansion model is given: the end of the
// last reply for a rewrite (L2), and more of it for the summary (L3).
const (
	replyWords         = 150
	summaryReplyWords  = 300
	summaryPromptWords = 150
)

// sessionInfo reads Claude Code's transcript for the last reply (L2) and
// keeps a running summary of the session (L3), for the expansion model
// (PRD Appendix 1, FR-6.3).
type sessionInfo struct {
	finder   *transcript.Finder // nil without a transcript folder
	lastSent func() string      // the last prompt sent, to tell sessions apart
	logf     func(string, ...any)
	every    time.Duration // how often run looks for a new reply

	mu         sync.Mutex
	path       string // the transcript last read, to log a change
	cached     cachedTurn
	summaryOf  string // the transcript the summary is of
	summary    string
	summarized string // the ID of the last turn in the summary
	failed     string // the ID of a turn the summary failed on: not retried
	holds      int    // rewrites running: the summary waits for them
	cancel     context.CancelFunc
}

type cachedTurn struct {
	path string
	size int64
	mod  time.Time
	turn transcript.Turn
	err  error
}

func newSessionInfo(started time.Time, lastSent func() string, logf func(string, ...any)) *sessionInfo {
	s := &sessionInfo{lastSent: lastSent, logf: logf, every: 2 * time.Second}
	root, err := transcript.Root()
	if err != nil {
		logf("context: no transcripts: %v", err)
		return s
	}
	dir, _ := os.Getwd()
	s.finder = &transcript.Finder{Root: root, Cwd: dir, Since: started}
	return s
}

// turn returns the last turn in the session's transcript.
func (s *sessionInfo) turn() (transcript.Turn, bool) {
	if s.finder == nil {
		return transcript.Turn{}, false
	}
	path := s.finder.Path(s.lastSent())
	s.mu.Lock()
	defer s.mu.Unlock()
	if path != s.path {
		if path == "" {
			s.logf("context: no transcript of this session found under %s", s.finder.Root)
		} else {
			s.logf("context: reading the transcript %s", path)
		}
		s.path = path
	}
	if path == "" {
		return transcript.Turn{}, false
	}
	if path != s.summaryOf {
		// Another transcript: after /clear, claude starts one with none of
		// the earlier conversation. Seen live: the summary kept "three
		// fruits … still open" after /clear, which claude no longer knew.
		if s.summary != "" {
			s.logf("context: summary cleared: the session has a new transcript")
		}
		s.summaryOf, s.summary, s.summarized, s.failed = path, "", "", ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return transcript.Turn{}, false
	}
	c := s.cached
	if c.path != path || c.size != info.Size() || !c.mod.Equal(info.ModTime()) {
		t, err := transcript.LastTurn(path)
		if err != nil && (c.path != path || c.err == nil) {
			s.logf("context: can't read the transcript: %v", err)
		}
		c = cachedTurn{path: path, size: info.Size(), mod: info.ModTime(), turn: t, err: err}
		s.cached = c
	}
	return c.turn, c.err == nil
}

// reply returns the end of Claude Code's last reply, or "" if there's
// none yet (L2).
func (s *sessionInfo) reply() string {
	t, ok := s.turn()
	if !ok {
		return ""
	}
	return transcript.Shorten(t.Reply, replyWords)
}

// Summary returns the session summary so far (L3).
func (s *sessionInfo) Summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summary
}

// hold keeps the summary from running while a rewrite does, cancelling
// one in progress: the model answers one request at a time, and a
// rewrite is waited for. It returns the release.
func (s *sessionInfo) hold() func() {
	s.mu.Lock()
	s.holds++
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.holds--
			s.mu.Unlock()
		})
	}
}

// run updates the summary after each reply while expand.context is
// "session", until ctx ends.
func (s *sessionInfo) run(ctx context.Context, current func() config.Config) {
	tick := time.NewTicker(s.every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		cfg := current()
		if cfg.Expand.Context != expand.ContextSession {
			continue
		}
		t, ok := s.turn()
		if !ok || !t.Done || t.Reply == "" {
			continue
		}
		s.mu.Lock()
		if s.holds > 0 || t.ID == s.summarized || t.ID == s.failed {
			s.mu.Unlock()
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, cfg.LLM.ExpandTimeout)
		s.cancel = cancel
		prev, of := s.summary, s.summaryOf
		s.mu.Unlock()

		start := time.Now()
		summary, err := summarize(sctx, cfg, prev, t)
		cancel()
		s.mu.Lock()
		s.cancel = nil
		switch {
		case s.summaryOf != of:
			// The transcript changed while the summary ran: it is of the old one.
		case errors.Is(err, context.Canceled) && ctx.Err() == nil:
			s.logf("context: summary paused for a rewrite; it runs again after")
		case err != nil:
			s.failed = t.ID
			s.logf("context: summary not updated: %v", err)
		default:
			s.summary, s.summarized = summary, t.ID
			s.logf("context: summary updated in %s (%d words): %q",
				time.Since(start).Round(time.Millisecond), len(strings.Fields(summary)), summary)
		}
		s.mu.Unlock()
	}
}

// summarize returns prev updated with the turn t. With the fake provider
// it is a fixed text, so tests are repeatable.
func summarize(ctx context.Context, cfg config.Config, prev string, t transcript.Turn) (string, error) {
	prompt := transcript.Shorten(t.Prompt, summaryPromptWords)
	if cfg.LLM.Provider == "fake" || os.Getenv(envSuggester) == "fake" {
		return strings.TrimSpace(prev + " Asked: " + prompt + "."), nil
	}
	e := &expand.Expander{Endpoint: cfg.LLM.Endpoint, Model: cfg.LLM.ExpandModel, KeepAlive: "30m"}
	return e.Summarize(ctx, prev, prompt, transcript.Shorten(t.Reply, summaryReplyWords))
}
