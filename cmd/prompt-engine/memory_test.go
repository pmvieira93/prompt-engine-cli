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
	prev := memorySample{at: at, cycles: 100, cpuSeconds: 10, gcSeconds: 1, availableSeconds: 1000, idleSeconds: 900}
	// Over 5 minutes: 12 s of CPU; of the runtime's 3000 s available, 2980
	// idle, so 20 s busy, 3 of them in the GC.
	cur := memorySample{at: at.Add(5 * time.Minute), live: 12 << 20, heap: 15 << 19, total: 30 << 20,
		cycles: 700, goroutines: 21, cpuSeconds: 22, gcSeconds: 4, availableSeconds: 4000, idleSeconds: 3880}
	want := "memory: live 12.0 MB, heap 7.5 MB, from the OS 30.0 MB; 21 goroutines; CPU 4.0% of a core, 15% of it in the GC, 120 GC cycles/min"
	if got := memoryLine(prev, cur); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// The GC's share is of the runtime's estimates only, which Go says
	// aren't comparable with system CPU time: an estimate larger than the
	// CPU measured still gives a share, not "GC 60%" of "CPU 40%".
	big := cur
	big.cpuSeconds, big.gcSeconds = 14, 13 // 4 s measured; 12 s of GC in 20 busy
	if got := memoryLine(prev, big); !strings.Contains(got, "CPU 1.3% of a core, 60% of it in the GC") {
		t.Errorf("an overestimated GC: %q", got)
	}
	// No Go code ran: no share to give.
	idle := cur
	idle.idleSeconds = prev.idleSeconds + 3000
	if got := memoryLine(prev, idle); strings.Contains(got, "in the GC") {
		t.Errorf("nothing ran, yet %q", got)
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
	// Cancel after a tick's line, not after a fixed time: -race on a slow
	// runner can delay the ticker.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(lines)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tick line within 5s: %d lines", n)
		}
		time.Sleep(time.Millisecond)
	}
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
