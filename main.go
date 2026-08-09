// Command slivingdoc is the standalone notebook synchronization server.
//
// `slivingdoc serve` exposes exactly two MCP tools — notes_pull(path) and
// notes_commit(path, message) — over stdio (architecture sections 2, 17,
// and 18). Startup resolves flags and the environment, opens the pinned
// native libgit2 engine, proves the S3 compatibility probe, and then
// accepts tool calls. `slivingdoc version` touches none of that.
//
// Stdout carries only MCP protocol messages and command output (usage,
// help, and the version line); logs and diagnostics go to stderr.
package main

import (
	"context"
	"os"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/cli"
	"github.com/baalimago/slivingdoc/internal/git2"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args, git2.New(), app.ProcessOptions{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}))
}
