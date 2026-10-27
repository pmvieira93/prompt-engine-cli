package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

func injector(t *testing.T, template string) (*Injector, *[]string) {
	t.Helper()
	dir := t.TempDir()
	if template != "" {
		if err := os.WriteFile(filepath.Join(dir, AppendFile), []byte(template), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var logs []string
	return &Injector{Dir: dir, Logf: func(f string, a ...any) { logs = append(logs, f) }}, &logs
}

func state(text string) input.State {
	return input.State{Text: text, Cursor: len([]rune(text)), Synced: true}
}

func TestBeforeSubmitAppends(t *testing.T) {
	in, _ := injector(t, "\r\n- Keep answers short.\r\n- Ask before \x1b[201~editing.\n\n")
	got := string(in.BeforeSubmit(state("explain the tracker")))
	want := "\x1b[200~\n\n- Keep answers short.\n- Ask before [201~editing.\x1b[201~"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestBeforeSubmitMovesToEnd(t *testing.T) {
	in, _ := injector(t, "rules")
	s := input.State{Text: "fix 👍🏽 bug", Cursor: 4, Synced: true} // cursor before the emoji
	got := string(in.BeforeSubmit(s))
	if !strings.HasPrefix(got, strings.Repeat("\x1b[C", 5)+"\x1b[200~") {
		t.Fatalf("expected 5 Right presses (emoji, space, b, u, g) then the paste, got %q", got)
	}
}

func TestBeforeSubmitSkips(t *testing.T) {
	cases := map[string]input.State{
		"empty":         state("   "),
		"slash command": state("/compact"),
		"bash mode":     state("!ls"),
		"memory note":   state("# use tabs"),
		"out of sync":   {Text: "abc", Cursor: 3},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			in, logs := injector(t, "rules")
			if got := in.BeforeSubmit(s); got != nil {
				t.Fatalf("injected %q", got)
			}
			if len(*logs) != 1 || !strings.Contains((*logs)[0], "not added") {
				t.Fatalf("logs = %q", *logs)
			}
		})
	}
	// Enter after a mention submits in claude, so it gets rules too.
	for _, text := range []string{"look at @inter", "look at @internal/rules "} {
		in, _ := injector(t, "rules")
		if in.BeforeSubmit(state(text)) == nil {
			t.Errorf("%q: rules not added", text)
		}
	}
}

func TestBeforeSubmitNeedsReady(t *testing.T) {
	in, _ := injector(t, "rules")
	in.Ready = func() bool { return false }
	if got := in.BeforeSubmit(state("hello")); got != nil {
		t.Fatalf("injected while not ready: %q", got)
	}
}

func TestBeforeSubmitWithoutTemplate(t *testing.T) {
	in, logs := injector(t, "")
	if got := in.BeforeSubmit(state("hello")); got != nil || len(*logs) != 0 {
		t.Fatalf("got %q, logs %q", got, *logs)
	}
	blank, _ := injector(t, " \n\t\n")
	if got := blank.BeforeSubmit(state("hello")); got != nil {
		t.Fatalf("blank template injected %q", got)
	}
}

func TestBeforeSubmitRereadsTemplate(t *testing.T) {
	in, _ := injector(t, "first")
	if got := string(in.BeforeSubmit(state("a"))); !strings.Contains(got, "first") {
		t.Fatalf("got %q", got)
	}
	if err := os.WriteFile(filepath.Join(in.Dir, AppendFile), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := string(in.BeforeSubmit(state("a"))); !strings.Contains(got, "second") {
		t.Fatalf("edit not picked up: %q", got)
	}
}

func TestBeforeSubmitRejectsHugeTemplate(t *testing.T) {
	in, logs := injector(t, strings.Repeat("x", maxSize+1))
	if got := in.BeforeSubmit(state("a")); got != nil || !strings.Contains(strings.Join(*logs, ""), "not added") {
		t.Fatalf("got %d bytes, logs %q", len(got), *logs)
	}
}
