package pluginembed

import "embed"

// Files is an embed.FS containing the full plugin tree:
//
//	.claude-plugin/plugin.json
//	hooks/hooks.json
//	scripts/*.sh
//
// The all: prefix is required for .claude-plugin because Go's embed
// excludes files whose names start with '.' by default.
//
//go:embed all:.claude-plugin hooks scripts
var Files embed.FS
