package webassets

import "embed"

// Files contém templates e assets estáticos incorporados ao binário.
//
//go:embed templates static
var Files embed.FS
