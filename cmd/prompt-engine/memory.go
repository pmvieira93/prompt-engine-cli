package main

import (
	"context"
	"fmt"
	"runtime/metrics"
	"syscall"
	"time"
)

// memoryEvery is how often the debug log gets a memory line, with the CPU
// used since the last one: often enough to place a heap that grows over
// days next to what was being done, rare enough to add ~300 lines a day.
const memoryEvery = 5 * time.Minute

// memoryMetrics are read without stopping the program, unlike
// runtime.ReadMemStats.
var memoryMetrics = []string{
	"/gc/heap/live:bytes",                // marked live by the last GC
	"/memory/classes/heap/objects:bytes", // live and not yet swept
	"/memory/classes/total:bytes",        // mapped from the OS, all of it
	"/gc/cycles/total:gc-cycles",
	"/cpu/classes/gc/total:cpu-seconds",
	"/sched/goroutines:goroutines",
}

// memorySample is one reading of memoryMetrics.
type memorySample struct {
	at                time.Time
	live, heap, total uint64
	cycles            uint64
	gcSeconds         float64
	cpuSeconds        float64 // the whole process, user and system, as ps counts it
	goroutines        uint64
}

func readMemory() memorySample {
	s := make([]metrics.Sample, len(memoryMetrics))
	for i, name := range memoryMetrics {
		s[i].Name = name
	}
	metrics.Read(s)
	u := func(i int) uint64 {
		if s[i].Value.Kind() == metrics.KindUint64 {
			return s[i].Value.Uint64()
		}
		return 0
	}
	var gc float64
	if s[4].Value.Kind() == metrics.KindFloat64 {
		gc = s[4].Value.Float64()
	}
	return memorySample{at: time.Now(), live: u(0), heap: u(1), total: u(2), cycles: u(3), gcSeconds: gc,
		cpuSeconds: processCPU(), goroutines: u(5)}
}

// processCPU is the CPU time the process has used, user and system. The
// runtime's metrics count only the Go code's share, not system calls such
// as reading the terminal.
func processCPU() float64 {
	var r syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &r) != nil {
		return 0
	}
	return time.Duration(r.Utime.Nano() + r.Stime.Nano()).Seconds()
}

// memoryLine describes cur, with the CPU used since prev, as a percentage
// of one core like ps shows, and the GC's share of it. A GC that spins
// shows as most of the CPU and many cycles a minute: seen, a session whose
// live heap passed a 28 MB memory limit used 350% CPU in the GC.
func memoryLine(prev, cur memorySample) string {
	mb := func(b uint64) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }
	live := mb(cur.live)
	if cur.cycles == 0 {
		live = "unknown (no GC yet)" // the GC measures it
	}
	line := fmt.Sprintf("memory: live %s, heap %s, from the OS %s; %d goroutines",
		live, mb(cur.heap), mb(cur.total), cur.goroutines)
	if seconds := cur.at.Sub(prev.at).Seconds(); seconds > 0 {
		line += fmt.Sprintf("; CPU %.1f%% of a core, GC %.1f%%, %.0f GC cycles/min",
			100*(cur.cpuSeconds-prev.cpuSeconds)/seconds, 100*(cur.gcSeconds-prev.gcSeconds)/seconds,
			float64(cur.cycles-prev.cycles)/seconds*60)
	}
	return line
}

// watchMemory logs a memory line every `every` until ctx ends, and one
// more then.
func watchMemory(ctx context.Context, every time.Duration, logf func(string, ...any)) {
	prev := readMemory()
	logf("%s", memoryLine(prev, prev))
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logf("%s", memoryLine(prev, readMemory()))
			return
		case <-tick.C:
		}
		cur := readMemory()
		logf("%s", memoryLine(prev, cur))
		prev = cur
	}
}
