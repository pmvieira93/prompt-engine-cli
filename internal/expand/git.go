package expand

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// maxStaged bounds how many staged files are listed.
const maxStaged = 10

// GitContext returns a Context func describing the git repository at dir:
// the current branch and the staged changes. Outside a repository, or if
// git isn't installed, it returns no lines.
func GitContext(dir string) func(ctx context.Context) []string {
	return func(ctx context.Context) []string {
		branch, err := git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return nil
		}
		lines := []string{"Branch: " + branch}
		if staged := stagedSummary(ctx, dir); staged != "" {
			lines = append(lines, "Staged changes: "+staged)
		}
		return lines
	}
}

// stagedSummary lists staged files with their added and removed line
// counts, e.g. "a.go (+10 −2), b.go (+1 −0)".
func stagedSummary(ctx context.Context, dir string) string {
	out, err := git(ctx, dir, "diff", "--cached", "--numstat")
	if err != nil || out == "" {
		return ""
	}
	var files []string
	rows := strings.Split(out, "\n")
	for _, row := range rows[:min(len(rows), maxStaged)] {
		added, rest, _ := strings.Cut(row, "\t")
		removed, name, _ := strings.Cut(rest, "\t")
		if added == "-" { // binary file
			files = append(files, name+" (binary)")
			continue
		}
		files = append(files, fmt.Sprintf("%s (+%s −%s)", name, added, removed))
	}
	if extra := len(rows) - maxStaged; extra > 0 {
		files = append(files, fmt.Sprintf("and %d more", extra))
	}
	return strings.Join(files, ", ")
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
