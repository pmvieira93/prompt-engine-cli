package suggest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeOllama serves /api/generate, recording requests and replying with
// reply (a JSON body) and status.
func fakeOllama(t *testing.T, status int, reply string) (*Ollama, *[]generateRequest) {
	t.Helper()
	var got []generateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		got = append(got, req)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &Ollama{Endpoint: srv.URL + "/", Model: "qwen2.5-coder:1.5b", MaxTokens: 8, KeepAlive: "30m"}, &got
}

func TestOllamaSuggest(t *testing.T) {
	o, reqs := fakeOllama(t, http.StatusOK, `{"response":"ctor the parser  "}`)
	got, err := o.Suggest(context.Background(), "refa", 4)
	if err != nil || got != "ctor the parser" {
		t.Fatalf("Suggest = %q, %v", got, err)
	}
	req := (*reqs)[0]
	if req.Model != "qwen2.5-coder:1.5b" || req.Prompt != promptHeader+"refa" || req.Suffix != "\n" ||
		req.Stream || req.KeepAlive != "30m" || req.Options["num_predict"] != float64(8) {
		t.Fatalf("request = %+v", req)
	}
}

func TestOllamaSkipsWithoutRequest(t *testing.T) {
	o, reqs := fakeOllama(t, http.StatusOK, `{"response":"x"}`)
	for _, tc := range []struct {
		text   string
		cursor int
	}{{"refa", 2}, {"   ", 3}, {"", 0}} {
		if got, err := o.Suggest(context.Background(), tc.text, tc.cursor); got != "" || err != nil {
			t.Errorf("Suggest(%q, %d) = %q, %v", tc.text, tc.cursor, got, err)
		}
	}
	if len(*reqs) != 0 {
		t.Fatalf("made %d requests", len(*reqs))
	}
}

func TestOllamaErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		check  func(error) bool
	}{
		{"no FIM", http.StatusBadRequest, `{"error":"registry.ollama.ai/library/qwen2.5:1.5b does not support insert"}`,
			func(err error) bool { return errors.Is(err, ErrNoFIM) }},
		{"model missing", http.StatusNotFound, `{"error":"model \"x\" not found, try pulling it first"}`,
			func(err error) bool { return strings.Contains(err.Error(), "try pulling it first") }},
		{"bad status", http.StatusInternalServerError, `{}`,
			func(err error) bool { return strings.Contains(err.Error(), "500") }},
		{"not JSON", http.StatusBadGateway, `<html>`,
			func(err error) bool { return strings.Contains(err.Error(), "502") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := fakeOllama(t, tc.status, tc.reply)
			if _, err := o.Suggest(context.Background(), "refa", 4); err == nil || !tc.check(err) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestOllamaHonorsCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	o := &Ollama{Endpoint: srv.URL, Model: "m", MaxTokens: 8}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := o.Suggest(ctx, "refa", 4); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation didn't interrupt the request")
	}
}

func TestClean(t *testing.T) {
	cases := []struct{ text, out, want string }{
		{"refa", "ctor the parser", "ctor the parser"},
		{"refactor the", " parser to use", " parser to use"},
		{"why does the ", " ghost text", "ghost text"},
		{"impl", "ement it\nmore lines", "ement it"},
		{"fix", "es  \t", "es"},
	}
	for _, tc := range cases {
		if got := clean(tc.text, tc.out); got != tc.want {
			t.Errorf("clean(%q, %q) = %q, want %q", tc.text, tc.out, got, tc.want)
		}
	}
}
