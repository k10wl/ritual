// Package ritual exposes the embedded frontend asset FS to cmd/gui.
package ritual

import "embed"

// GUIAssets is the embedded frontend bundle served by the Wails app.
//
//go:embed all:frontend/dist
var GUIAssets embed.FS
