// Package webassets provides the reviewed publisher artwork to the backend.
package webassets

import "embed"

//go:embed public/logos/companies public/logos/sources
var Logos embed.FS
