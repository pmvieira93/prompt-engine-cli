package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if got.Total != want || got.Turn != want || got.Context != 1151 || got.Model != "claude-opus-5-5" {
		t.Fatalf("first read: %+v", got)
	}
	if ended := r.Ended(); ended != nil {
		t.Errorf("a turn ended before the next prompt: %+v", ended)
	}
	// While the transcript is being looked for again, the tally stays.
	if again, _ := r.Read(""); again != got {
		t.Errorf("empty path: %+v", again)
	}

	// The next prompt ends the turn; a line still being written waits.
	appendTo(t, path, prompt2+"\n"+call("m3", "end_turn", 1, 0, 1200, 5)[:40])
	if got, _ = r.Read(path); got.Turn != (Usage{}) || got.Total != want {
		t.Fatalf("after a prompt: %+v", got)
	}
	if ended := r.Ended(); len(ended) != 1 || ended[0].Prompt != "why does the parser crash" || ended[0].Usage != want || ended[0].Context != 1151 {
		t.Fatalf("ended: %+v", ended)
	}
	appendTo(t, path, call("m3", "end_turn", 1, 0, 1200, 5)[40:]+"\n")
	got, _ = r.Read(path)
	if got.Turn != (Usage{Calls: 1, Input: 1, CacheRead: 1200, Output: 5}) || got.Total.Calls != 3 || got.Context != 1201 {
		t.Fatalf("after the line ended: %+v", got)
	}
	// Neither a notice nor an interruption is a prompt: the turn goes on.
	appendTo(t, path, notice+"\n"+`{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}`+"\n")
	if got, _ = r.Read(path); got.Turn.Calls != 1 || r.Ended() != nil {
		t.Fatalf("after a notice and an interruption: %+v", got)
	}

	// Subagents count apart, from their own transcripts.
	sub := filepath.Join(dir, "s1", "subagents", "agent-a1.jsonl")
	subLines := []string{
		`{"type":"user","isSidechain":true,"message":{"content":"look into it"}}`,
		`{"type":"assistant","isSidechain":true,` + call("sa", "end_turn", 5, 10, 20, 7)[len(`{"type":"assistant",`):],
	}
	write(t, sub, subLines...)
	got, _ = r.Read(path)
	if got.Subagents != (Usage{Calls: 1, Input: 5, CacheWrite: 10, CacheRead: 20, Output: 7}) || got.TurnSubagents != 1 || got.Total.Calls != 3 || got.Turn.Calls != 1 {
		t.Fatalf("subagents: %+v", got)
	}
	if again, _ := r.Read(path); again != got {
		t.Errorf("nothing new, but %+v != %+v", again, got)
	}
	// A subagent's file written again from the start isn't counted twice.
	write(t, sub, subLines[1])
	if again, _ := r.Read(path); again.Subagents.Calls != 1 {
		t.Errorf("subagent file rewritten: %+v", again.Subagents)
	}

	// A prompt that starts with a paste is a prompt; the turn it ends
	// carries the subagents' call.
	appendTo(t, path, `{"type":"user","message":{"content":"<pasted_content id=\"ab12\">log</pasted_content id=\"ab12\"> why?"}}`+"\n")
	if got, _ = r.Read(path); got.TurnSubagents != 0 || got.Subagents.Calls != 1 || got.Turn.Calls != 0 {
		t.Fatalf("a prompt starting with a paste: %+v", got)
	}
	if ended := r.Ended(); len(ended) != 1 || ended[0].Prompt != "fix that" || ended[0].Usage.Calls != 1 || ended[0].Subagents != 1 {
		t.Fatalf("ended: %+v", ended)
	}
	if _, any := r.Current(); any {
		t.Error("the new turn has no calls yet")
	}
	appendTo(t, path, call("m4", "end_turn", 1, 0, 10, 1)+"\n")
	r.Read(path)
	if cur, any := r.Current(); !any || cur.Usage.Calls != 1 || !strings.HasPrefix(cur.Prompt, "<pasted_content") {
		t.Errorf("current: %+v %v", cur, any)
	}

	// Another transcript, as after /clear, starts again.
	other := filepath.Join(dir, "s2.jsonl")
	write(t, other, prompt1, call("n1", "end_turn", 1, 2, 3, 4))
	if got, _ = r.Read(other); got.Total.Calls != 1 || got.Subagents.Calls != 0 || got.Context != 6 {
		t.Fatalf("new transcript: %+v", got)
	}
	if _, err := r.Read(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Error("a missing transcript: no error")
	}
}
