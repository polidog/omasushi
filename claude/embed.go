// Package claude carries the omasushi skill so the binary can install it
// (`omasushi skill install`) without a checkout of this repository.
package claude

import "embed"

// Skills holds skills/<name>/... — the same tree claude/omasushi.yaml links.
//
//go:embed skills
var Skills embed.FS
