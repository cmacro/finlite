package main

import (
	"embed"
	"log"

	"finlite/internal/app"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.
//
// NOTE: this must stay in the project root: go:embed patterns cannot
// reference parent directories (no `..`), so internal/app cannot embed
// frontend/dist directly. main passes the FS into app.Run.

//go:embed all:frontend/dist
var assets embed.FS

// main function serves as the application's entry point.
// All setup lives in internal/app; keep this file thin so the
// project root stays clean as features grow.
func main() {
	if err := app.Run(assets); err != nil {
		log.Fatal(err)
	}
}
