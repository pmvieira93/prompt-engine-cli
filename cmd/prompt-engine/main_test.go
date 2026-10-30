package main

import (
	"slices"
	"strings"
	"testing"
)

func TestOwnFlags(t *testing.T) {
	cases := []struct {
		args     []string
		rest     []string
		cfg      string
		wantsErr bool
	}{
		{[]string{"--resume", "-p", "hi"}, []string{"--resume", "-p", "hi"}, "", false},
		{[]string{"--pe-config", "/tmp/a.yaml", "--resume"}, []string{"--resume"}, "/tmp/a.yaml", false},
		{[]string{"-p", "hi", "--pe-config=/tmp/b.yaml"}, []string{"-p", "hi"}, "/tmp/b.yaml", false},
		{[]string{"--", "--pe-config", "x"}, []string{"--", "--pe-config", "x"}, "", false}, // after --: for the child
		{[]string{"--pe-config"}, nil, "", true},
		{[]string{"--pe-config="}, nil, "", true},
		{nil, nil, "", false},
	}
	for _, tc := range cases {
		rest, cfg, err := ownFlags(tc.args)
		if (err != nil) != tc.wantsErr || cfg != tc.cfg || !slices.Equal(rest, tc.rest) {
			t.Errorf("ownFlags(%q) = %q, %q, %v; want %q, %q, error %v", tc.args, rest, cfg, err, tc.rest, tc.cfg, tc.wantsErr)
		}
	}
}

func TestWantsVersion(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"--pe-version"}, true},
		{[]string{"--resume", "--pe-version"}, true},
		{[]string{"--", "--pe-version"}, false}, // for the child
		{[]string{"--version"}, false},          // claude's own
		{nil, false},
	} {
		if got := wantsVersion(tc.args); got != tc.want {
			t.Errorf("wantsVersion(%q) = %v", tc.args, got)
		}
	}
}

func TestGuardNotes(t *testing.T) {
	got := guardNotes("Guard ", "added-task", "ensure, runs", "Ensure the code runs without errors.\nCheck for the same error message, code and environment.")
	want := []string{"Guard added-task: ensure, runs", "Rejected: “Ensure the code runs without errors. Check for…”"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("guardNotes = %q\nwant         %q", got, want)
	}
	if got := guardNotes("Guard ", "empty", "", "")[0]; got != "Guard empty" {
		t.Errorf("without a detail: %q", got)
	}
	if got := shorten("short", 50); got != "short" {
		t.Errorf("shorten kept %q", got)
	}
}
