package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	finder  *transcript.Finder // nil without a transcript folder
	sent    func() []string    // the last prompts sent since the start or /clear, to tell sessions apart
	onClear func(t time.Time)  // after /clear at t: forget the prompts sent before it
	logf    func(string, ...any)
	every   time.Duration // how often run looks for a new reply

	mu         sync.Mutex
	path       string // the transcript last read, to log a change
	cached     cachedTurn
	summaryOf  string // the transcript the summary is of
	summary    string
	summarized string // the ID of the last turn in the summary
	failed     string // the ID of a turn the summary failed on: not retried
	holds      int    // rewrites running: the summary waits for them
	cleared    int    // how many times /clear was seen: a turn read before one is stale
	cancel     context.CancelFunc

	// The tokens of the conversation so far (PRD Appendix 5, FR-11.1),
	// for the panel; nil before a transcript is found.
	tally   atomic.Pointer[transcript.Tally]
	onReply func(transcript.Tally) // when a reply ends, for the history
	redraw  func()                 // when the tally changes, for the panel
}

type cachedTurn struct {
	path string
	size int64
	mod  time.Time
	turn transcript.Turn
	err  error
}

func newSessionInfo(started time.Time, sent func() []string, onClear func(time.Time), logf func(string, ...any)) *sessionInfo {
	s := &sessionInfo{sent: sent, onClear: onClear, logf: logf, every: 2 * time.Second}
	root, err := transcript.Root()
	if err != nil {
		logf("context: no transcripts: %v", err)
		return s
	}
	dir, _ := os.Getwd()
	s.finder = &transcript.Finder{Root: root, Cwd: dir, Since: started}
	return s
}

// transcriptPath returns the session's transcript, or "" before one is
// found. Call it without s.mu held.
func (s *sessionInfo) transcriptPath() string {
	if s.finder == nil {
		return ""
	}
	s.mu.Lock()
	cleared := s.cleared
	s.mu.Unlock()
	path := s.finder.Path(s.sent()...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleared != cleared {
		return "" // found by prompts from before /clear
	}
	if path != s.path {
		if path == "" {
			s.logf("context: no transcript of this session found under %s", s.finder.Root)
		} else {
			s.logf("context: reading the transcript %s", path)
		}
		s.path = path
	}
	return path
}

// turn returns the last turn in the session's transcript.
func (s *sessionInfo) turn() (transcript.Turn, bool) {
	path := s.transcriptPath()
	if path == "" {
		return transcript.Turn{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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

// slashCommand is told when a slash command is sent, at t. If it was
// /clear, Claude Code starts a new transcript within a moment: then the
// earlier reply, summary and prompts are forgotten, since claude no longer
// knows them. Seen: a rewrite right after /clear still used the reply
// from before it. Waiting for the transcript, rather than reading what
// was typed, also catches /clear picked from the menu after "/cl".
func (s *sessionInfo) slashCommand(t time.Time) {
	if s.finder == nil {
		return
	}
	go func() {
		// Read here, not at Enter: on a miss, it reads every candidate.
		current := s.finder.Path(s.sent()...)
		for range 20 {
			time.Sleep(250 * time.Millisecond)
			if !s.finder.ClearedAfter(t, current) {
				continue
			}
			// First what finds the old transcript, then what was read from
			// it: a turn() in between finds nothing (cleared changed).
			if s.onClear != nil {
				s.onClear(t)
			}
			s.finder.Forget()
			s.mu.Lock()
			s.cleared++
			s.path, s.summaryOf, s.summary, s.summarized, s.failed = "", "", "", "", ""
			s.cached = cachedTurn{}
			s.mu.Unlock()
			s.logf("context: /clear: the earlier reply, summary and prompts are no longer used")
			return
		}
	}()
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

// watchUsage reads the conversation's token usage from its transcript
// every s.every, until ctx ends (PRD Appendix 5, FR-11.1). It runs
// whatever expand.context is: the panel shows the tokens either way.
func (s *sessionInfo) watchUsage(ctx context.Context) {
	if s.finder == nil {
		return
	}
	tick := time.NewTicker(s.every)
	defer tick.Stop()
	var r transcript.UsageReader
	var failed, reported string // the transcript a read failed on; the last reply recorded
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		path := s.transcriptPath()
		t, err := r.Read(path)
		if err != nil {
			if failed != path {
				s.logf("usage: can't read every call: %v", err)
			}
			failed = path
		}
		var next *transcript.Tally
		if path != "" {
			next = &t
		}
		if prev := s.tally.Swap(next); (prev == nil) != (next == nil) || (prev != nil && *prev != t) {
			if s.redraw != nil {
				s.redraw()
			}
		}
		if next == nil {
			continue
		}
		// A reply ends once: the next prompt starts a new turn.
		key := fmt.Sprintf("%s %d", path, t.Total.Calls)
		if !t.TurnDone || t.Turn.Calls == 0 || key == reported {
			continue
		}
		reported = key
		s.logf("usage: reply %d %s, %s; context %s, %s; subagent calls %d",
			t.Turn.Calls, plural(t.Turn.Calls, "call"), usageLine(t.Turn), tokens(t.Context), t.Model, t.TurnSubagents)
		if s.onReply != nil {
			s.onReply(t)
		}
	}
}

// usageLines are the panel's lines about the tokens used, or none before
// the first reply. They fit a panel 44 columns wide.
func (s *sessionInfo) usageLines() []string {
	t := s.tally.Load()
	if t == nil || t.Total.Calls == 0 {
		return nil
	}
	total := t.Total
	total.Add(t.Subagents)
	lines := []string{
		"tokens:      context " + tokens(t.Context) + " · " + t.Model,
		"  reply:     " + usageLine(t.Turn),
		"  so far:    " + usageLine(total),
		fmt.Sprintf("  calls:     %d in the reply, %d so far", t.Turn.Calls, total.Calls),
	}
	if t.Subagents.Calls > 0 {
		lines = append(lines, fmt.Sprintf("             incl. %d by subagents", t.Subagents.Calls))
	}
	return lines
}

func usageLine(u transcript.Usage) string {
	if u.Calls == 0 {
		return "waiting"
	}
	return fmt.Sprintf("%s in (%s cached), %s out", tokens(u.In()), cached(u), tokens(u.Output))
}

// cached is the share of u's input read from the cache.
func cached(u transcript.Usage) string {
	if u.In() == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", u.CacheRead*100/u.In())
}

// tokens is n shortened: 950, 1.2k, 276k, 1.4M.
func tokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 1_000_000:
		return fmt.Sprintf("%dk", n/1000)
	case n < 10_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	default:
		return fmt.Sprintf("%dM", n/1_000_000)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
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
