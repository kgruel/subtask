package plugin

import "embed"

// FS contains the embedded Claude plugin files.
//
// Install logic should typically use fs.WalkDir(FS, ".") and copy files into a target plugin directory,
// preserving the relative paths.
//
//go:embed .claude-plugin/plugin.json commands/* hooks/hooks.json scripts/*
var FS embed.FS
