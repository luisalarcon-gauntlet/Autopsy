// Package main embeds the templates directory into the binary at compile time.
// This ensures the container has zero external file dependencies at runtime.
package main

import "embed"

//go:embed templates
var templateFS embed.FS
