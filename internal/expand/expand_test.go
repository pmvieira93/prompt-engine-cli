package expand

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fakeChat(t *testing.T, reply string) (*Expander, *chatRequest) {
	t.Helper()
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &Expander{Endpoint: srv.URL, Model: "qwen2.5:7b", KeepAlive: "30m"}, &got
}

func TestExpandRewritesAndAppendsContext(t *testing.T) {
	e, req := fakeChat(t, `{"message":{"role":"assistant","content":"  Fix the failing test in the config package.\n"}}`)
	e.Context = func(context.Context) []string { return []string{"Branch: main", "Staged changes: a.go (+1 −0)"} }
	got, err := e.Expand(context.Background(), "fix teh failing tst in config")
	if err != nil {
		t.Fatal(err)
	}
	want := "Fix the failing test in the config package.\n\nContext:\n- Branch: main\n- Staged changes: a.go (+1 −0)"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if req.Model != "qwen2.5:7b" || req.Messages[0].Content != DefaultSystem ||
		req.Messages[1].Content != "Request: fix teh failing tst in config" || req.Stream {
		t.Fatalf("request = %+v", *req)
	}
}

func TestExpandUsesSystemTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SystemFile), []byte("  Be terse.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, req := fakeChat(t, `{"message":{"content":"ok"}}`)
	e.TemplatesDir = dir
	if _, err := e.Expand(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if req.Messages[0].Content != "Be terse." {
		t.Fatalf("system = %q", req.Messages[0].Content)
	}
}

func TestExpandErrors(t *testing.T) {
	for name, reply := range map[string]string{
		"model error": `{"error":"model \"qwen2.5:7b\" not found, try pulling it first"}`,
		"empty":       `{"message":{"content":"   "}}`,
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := fakeChat(t, reply)
			if got, err := e.Expand(context.Background(), "x"); err == nil {
				t.Fatalf("got %q, want an error", got)
			}
		})
	}
}

func TestGitContext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	ctx := context.Background()
	if lines := GitContext(dir)(ctx); lines != nil {
		t.Fatalf("outside a repo: %q", lines)
	}

	run("init", "-q", "-b", "feature/x")
	// No commits yet, as in a fresh repository: still a branch and staged files.
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "a.go")
	if lines := GitContext(dir)(ctx); strings.Join(lines, "|") != "Branch: feature/x|Staged changes: a.go (+2 −0)" {
		t.Fatalf("before the first commit: %q", lines)
	}

	run("commit", "-q", "-m", "start")
	if lines := GitContext(dir)(ctx); strings.Join(lines, "|") != "Branch: feature/x" {
		t.Fatalf("clean repo: %q", lines)
	}

	run("checkout", "-q", "--detach")
	if lines := GitContext(dir)(ctx); len(lines) != 1 || !strings.HasPrefix(lines[0], "Branch: detached at ") {
		t.Fatalf("detached: %q", lines)
	}
}

func TestStripContext(t *testing.T) {
	cases := map[string]string{
		"Fix it.\n\nContext:\n- Branch: main\n- Staged changes: a.go (+1 −0)": "Fix it.",
		// I5's expansion with I6's request typed under its header.
		"Add tests.\n\nContext:\nrefactor tracker to use chans": "Add tests.\n\nrefactor tracker to use chans",
		"no context here":    "no context here",
		"- Branch: alone":    "",
		"keep - Branch: mid": "keep - Branch: mid",
	}
	for in, want := range cases {
		if got := StripContext(in); got != want {
			t.Errorf("StripContext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExpandStripsOldContext(t *testing.T) {
	e, req := fakeChat(t, `{"message":{"content":"Fix it."}}`)
	e.Context = func(context.Context) []string { return []string{"Branch: new"} }
	got, err := e.Expand(context.Background(), "fix it\n\nContext:\n- Branch: old")
	if err != nil {
		t.Fatal(err)
	}
	if req.Messages[1].Content != "Request: fix it" || got != "Fix it.\n\nContext:\n- Branch: new" {
		t.Fatalf("sent %q, got %q", req.Messages[1].Content, got)
	}
}

func TestExpandOnlyContext(t *testing.T) {
	e, _ := fakeChat(t, `{"message":{"content":"x"}}`)
	if got, err := e.Expand(context.Background(), "Context:\n- Branch: main"); err == nil {
		t.Fatalf("got %q, want an error", got)
	}
}

func TestExpandWithEarlierPrompts(t *testing.T) {
	e, req := fakeChat(t, `{"message":{"content":"Add tests for the retry logic."}}`)
	e.Earlier = func() []string { return []string{"add a retry to the client", "make it exponential"} }
	if _, err := e.Expand(context.Background(), "now add tests for it"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(req.Messages[0].Content, EarlierRule) {
		t.Errorf("system prompt lacks the rule for earlier requests: %q", req.Messages[0].Content)
	}
	want := "Earlier requests in this session (oldest first):\n- add a retry to the client\n- make it exponential\n\nRequest: now add tests for it"
	if req.Messages[1].Content != want {
		t.Errorf("request = %q\nwant      %q", req.Messages[1].Content, want)
	}

	// No earlier prompts: the request is sent as before, without the rule.
	e.Earlier = func() []string { return nil }
	if _, err := e.Expand(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if req.Messages[1].Content != "Request: x" || strings.Contains(req.Messages[0].Content, "Earlier requests") {
		t.Errorf("without earlier prompts: %+v", req.Messages)
	}
}
