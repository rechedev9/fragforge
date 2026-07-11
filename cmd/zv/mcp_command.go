package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/rechedev9/fragforge/internal/mcpserver"
)

// mcpVersion is the implementation version reported to MCP clients. The zv build
// carries no embedded version string today, so this is a stable placeholder.
const mcpVersion = "dev"

// runMCP starts the MCP stdio server. It stays thin: discovery and all tool
// logic live in internal/mcpserver. The stdio transport owns stdout (the
// JSON-RPC wire), so this path must print nothing else to stdout; diagnostics go
// to stderr.
func runMCP(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {}
	url := fs.String("url", "", "orchestrator base URL")
	token := fs.String("token", "", "X-FragForge-Token")
	help := fs.Bool("help", false, "show help")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, mcpUsage)
			return exitSuccess
		}
		fmt.Fprint(stderr, mcpUsage)
		return exitInvalidArgs
	}
	if *help {
		fmt.Fprint(stdout, mcpUsage)
		return exitSuccess
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, `error: unexpected extra args for "mcp"`)
		fmt.Fprint(stderr, mcpUsage)
		return exitInvalidArgs
	}

	err := mcpserver.Run(context.Background(), mcpserver.Options{
		URL:     *url,
		Token:   *token,
		Stderr:  stderr,
		Version: mcpVersion,
	})
	if err != nil {
		fmt.Fprintf(stderr, "zv mcp: %v\n", err)
		return exitUnexpected
	}
	return exitSuccess
}
