// Package docs builds the guard rails template into the binary, so
// prompt-engine --pe-init-guards can write it without a download.
package docs

import _ "embed"

// GuardsExample is guards.example.txt: all comments, so it changes nothing
// until a rule is added.
//
//go:embed guards.example.txt
var GuardsExample []byte
