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

func TestPastedOrLong(t *testing.T) {
	cases := map[string]bool{
		"fix teh failing tst in config":           false,
		"line one\nline two":                      false,
		"look at this:\n```\npanic: nil map\n```": true,
		"trace:\n1\n2\n3\n4\n5\n6":                true,
		strings.Repeat("word ", 90):               true,
	}
	for text, want := range cases {
		if got := pastedOrLong(text); got != want {
			t.Errorf("pastedOrLong(%q) = %v, want %v", text, got, want)
		}
	}
}
