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
		if _, err := git(ctx, dir, "rev-parse", "--git-dir"); err != nil {
			return nil // not a repository
		}
		var lines []string
		if branch := currentBranch(ctx, dir); branch != "" {
			lines = append(lines, "Branch: "+branch)
		}
		if staged := stagedSummary(ctx, dir); staged != "" {
			lines = append(lines, "Staged changes: "+staged)
		}
		return lines
	}
}

// currentBranch names the checked-out branch, including one with no
// commits yet (where "rev-parse HEAD" fails), or the commit when detached.
func currentBranch(ctx context.Context, dir string) string {
	if b, err := git(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && b != "" {
		return b
	}
	if c, err := git(ctx, dir, "rev-parse", "--short", "HEAD"); err == nil && c != "" {
		return "detached at " + c
	}
	return ""
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
