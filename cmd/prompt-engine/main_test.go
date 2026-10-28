package main

import (
	"slices"
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
