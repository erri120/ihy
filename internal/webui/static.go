// Embedded static assets for the web UI.
package webui

import "embed"

//go:embed static
var staticFS embed.FS
