package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// call is an assistant record with usage, as Claude Code 2.1.311 writes
// one: a reply in several records repeats its id and usage.
func call(id, stop string, in, write, read, out int64) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"model":"claude-opus-5-5","stop_reason":%q,"content":[],`+
		`"usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`,
		id, stop, in, write, read, out)
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestUsageReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	write(t, path,
		prompt1,
		call("m1", "tool_use", 2, 100, 1000, 10),
		call("m1", "tool_use", 2, 100, 1000, 10), // the same call, in a second record
		result,
		call("m2", "end_turn", 1, 50, 1100, 20),
		sidechain,
		`{"type":"assistant","isSidechain":true,"message":{"id":"x","usage":{"input_tokens":999}}}`,
		`{"type":"assistant","message":{"id":"e","model":"<synthetic>","usage":{"input_tokens":0}}}`,
		other,
	)
	var r UsageReader
	got, err := r.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{Calls: 2, Input: 3, CacheWrite: 150, CacheRead: 2100, Output: 30}
	if got.Total != want || got.Turn != want || !got.TurnDone || got.Context != 1151 || got.Model != "claude-opus-5-5" {
		t.Fatalf("first read: %+v", got)
	}

	// A new prompt starts a new turn; a line still being written waits.
	appendTo(t, path, prompt2+"\n"+call("m3", "end_turn", 1, 0, 1200, 5)[:40])
	if got, _ = r.Read(path); got.Turn != (Usage{}) || got.TurnDone || got.Total != want {
		t.Fatalf("after a prompt: %+v", got)
	}
	appendTo(t, path, call("m3", "end_turn", 1, 0, 1200, 5)[40:]+"\n")
	got, _ = r.Read(path)
	if got.Turn != (Usage{Calls: 1, Input: 1, CacheRead: 1200, Output: 5}) || got.Total.Calls != 3 || got.Context != 1201 || !got.TurnDone {
		t.Fatalf("after the line ended: %+v", got)
	}
	// A notice isn't a prompt: the turn goes on.
	appendTo(t, path, notice+"\n")
	if got, _ = r.Read(path); got.Turn.Calls != 1 {
		t.Fatalf("after a notice: %+v", got)
	}

	// Subagents count apart, from their own transcripts.
	sub := filepath.Join(dir, "s1", "subagents", "agent-a1.jsonl")
	write(t, sub,
		`{"type":"user","isSidechain":true,"message":{"content":"look into it"}}`,
		`{"type":"assistant","isSidechain":true,`+call("sa", "end_turn", 5, 10, 20, 7)[len(`{"type":"assistant",`):],
	)
	got, _ = r.Read(path)
	if got.Subagents != (Usage{Calls: 1, Input: 5, CacheWrite: 10, CacheRead: 20, Output: 7}) || got.TurnSubagents != 1 || got.Total.Calls != 3 || got.Turn.Calls != 1 {
		t.Fatalf("subagents: %+v", got)
	}
	if again, _ := r.Read(path); again != got {
		t.Errorf("nothing new, but %+v != %+v", again, got)
	}

	appendTo(t, path, prompt1+"\n")
	if got, _ = r.Read(path); got.TurnSubagents != 0 || got.Subagents.Calls != 1 {
		t.Fatalf("the next prompt: %+v", got)
	}

	// Another transcript, as after /clear, starts again.
	other := filepath.Join(dir, "s2.jsonl")
	write(t, other, prompt1, call("n1", "end_turn", 1, 2, 3, 4))
	if got, _ = r.Read(other); got.Total.Calls != 1 || got.Subagents.Calls != 0 || got.Context != 6 {
		t.Fatalf("new transcript: %+v", got)
	}
	if got, err := r.Read(""); err != nil || got != (Tally{}) {
		t.Errorf("no transcript: %+v, %v", got, err)
	}
	if _, err := r.Read(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Error("a missing transcript: no error")
	}
}
