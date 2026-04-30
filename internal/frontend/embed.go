package frontend

import "embed"

// Dist contains the production frontend build.
//
//go:embed dist
var Dist embed.FS
