package config

import (
	"context"
	"os"
	"time"
)

// Watch checks path every interval and, whenever the file changes, appears,
// or disappears, calls onChange with the result of loading it: the new
// config, or the defaults and an error (see LoadFile). A deleted file loads
// as the defaults. Watch returns when ctx is done.
//
// It polls rather than subscribing to file events: a config is edited
// rarely, a second's delay is fine, and polling needs no dependency and
// survives editors that replace the file instead of writing to it.
func Watch(ctx context.Context, path string, interval time.Duration, onChange func(Config, error)) {
	last := stampOf(path)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s := stampOf(path); s != last {
			last = s
			onChange(LoadFile(path))
		}
	}
}

// stamp identifies a version of a file well enough to notice edits.
type stamp struct {
	exists bool
	mod    time.Time
	size   int64
}

func stampOf(path string) stamp {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{exists: true, mod: info.ModTime(), size: info.Size()}
}
