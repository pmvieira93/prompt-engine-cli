package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryLine(t *testing.T) {
	at := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	prev := memorySample{at: at, cycles: 100, gcSeconds: 1, cpuSeconds: 10}
	cur := memorySample{at: at.Add(5 * time.Minute), live: 12 << 20, heap: 15 << 19, total: 30 << 20,
		cycles: 700, gcSeconds: 4, cpuSeconds: 22, goroutines: 21}
	want := "memory: live 12.0 MB, heap 7.5 MB, from the OS 30.0 MB; 21 goroutines; CPU 4.0% of a core, GC 1.0%, 120 GC cycles/min"
	if got := memoryLine(prev, cur); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// The first line has no interval, so no CPU.
	if got := memoryLine(cur, cur); strings.Contains(got, "CPU") {
		t.Errorf("first line %q has CPU", got)
	}
	// Before the first GC, the live heap isn't known: not 0 MB.
	if got := memoryLine(memorySample{at: at}, memorySample{at: at, heap: 1 << 20}); !strings.HasPrefix(got, "memory: live unknown (no GC yet), heap 1.0 MB") {
		t.Errorf("before a GC: %q", got)
	}
}

func TestReadMemory(t *testing.T) {
	runtime.GC() // live is measured by a GC
	m := readMemory()
	if m.live == 0 || m.heap == 0 || m.total == 0 || m.cpuSeconds == 0 || m.goroutines == 0 {
		t.Errorf("a metric read as zero: %+v", m)
	}
	if m.total < m.heap {
		t.Errorf("from the OS %d < heap %d", m.total, m.heap)
	}
}

func TestWatchMemory(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchMemory(ctx, 10*time.Millisecond, logf)
		close(done)
	}()
	time.Sleep(55 * time.Millisecond)
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	// One at start, one per tick, one at the end.
	if len(lines) < 3 {
		t.Fatalf("%d lines: %q", len(lines), lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "memory: live ") {
			t.Errorf("line %q", l)
		}
	}
	if !strings.Contains(lines[len(lines)-1], "% of a core") {
		t.Errorf("the last line has no CPU: %q", lines[len(lines)-1])
	}
}
