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

func added(in *Injector, s input.State) string {
	text, _ := in.Prepare(s)
	return text
}

func TestPrepareAddsHeading(t *testing.T) {
	in, _ := injector(t, "\r\n- Keep answers short.\r\n- Ask before editing.\n\n")
	got := added(in, state("explain the tracker"))
	want := "# Guidance\n- Keep answers short.\n- Ask before editing."
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestPrepareSkips(t *testing.T) {
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
			if got := added(in, s); got != "" {
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
		if added(in, state(text)) == "" {
			t.Errorf("%q: rules not added", text)
		}
	}
}

func TestPrepareSkipsShort(t *testing.T) {
	in, logs := injector(t, "rules")
	in.MinWords = func() int { return 3 }
	for _, text := range []string{"yes", "ok thanks", "  ok   thanks  "} {
		if got := added(in, state(text)); got != "" {
			t.Errorf("%q: injected %q", text, got)
		}
		if in.Text(text) != "" {
			t.Errorf("%q: the preview has the rules", text)
		}
	}
	if !strings.Contains((*logs)[0], "not added (short prompt)") {
		t.Errorf("logs = %q", *logs)
	}
	if added(in, state("run the tests")) == "" {
		t.Error("three words: rules not added")
	}
	if in.Text("run the tests") != Heading+"\nrules" {
		t.Error("three words: the preview has no rules")
	}
	// A rewrite's context lines aren't words of the prompt.
	rewrite := "Please ok.\n\nContext:\n- Branch: main\n- Staged changes: 1 file"
	if got := added(in, state(rewrite)); got != "" {
		t.Errorf("short rewrite: injected %q", got)
	}
	if in.Text(rewrite) != "" {
		t.Error("short rewrite: the preview has the rules")
	}
}

func TestPrepareNeedsReady(t *testing.T) {
	in, _ := injector(t, "rules")
	in.Ready = func() bool { return false }
	if got := added(in, state("hello")); got != "" {
		t.Fatalf("added while not ready: %q", got)
	}
}

func TestPrepareWithoutTemplate(t *testing.T) {
	in, logs := injector(t, "")
	if got := added(in, state("hello")); got != "" || len(*logs) != 0 {
		t.Fatalf("got %q, logs %q", got, *logs)
	}
	blank, _ := injector(t, " \n\t\n")
	if got := added(blank, state("hello")); got != "" {
		t.Fatalf("blank template injected %q", got)
	}
}

func TestPrepareRereadsTemplate(t *testing.T) {
	in, _ := injector(t, "first")
	if got := added(in, state("a")); !strings.Contains(got, "first") {
		t.Fatalf("got %q", got)
	}
	if err := os.WriteFile(filepath.Join(in.Dir, AppendFile), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := added(in, state("a")); !strings.Contains(got, "second") {
		t.Fatalf("edit not picked up: %q", got)
	}
}

func TestPrepareRejectsHugeTemplate(t *testing.T) {
	in, logs := injector(t, strings.Repeat("x", maxSize+1))
	if got := added(in, state("a")); got != "" || !strings.Contains(strings.Join(*logs, ""), "not added") {
		t.Fatalf("got %d bytes, logs %q", len(got), *logs)
	}
}
