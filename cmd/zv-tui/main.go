// Command zv-tui is a lazygit-style terminal UI for the FragForge pipeline. It
// is a thin client of the orchestrator HTTP API (the same surface the web Studio
// drives), so it runs the whole flow from a terminal: browse jobs, upload a
// demo, pick a player, record, compose, and render Shorts - plus the stream-clip
// flow. Reach it as "zv tui".
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

const usage = `zv-tui - lazygit-style terminal UI for the FragForge pipeline

Usage:
  zv tui [--url <orchestrator>] [--token <token>] [file ...]

Files given as arguments (or dragged onto the executable) are uploaded on
startup: .dem as demos, .mp4/.mov/.mkv/.webm as stream clips. Dropping a file
onto the running TUI uploads it too.

Flags:
  --url <addr>     orchestrator base URL (default $ORCHESTRATOR_URL or ` + tuiclient.DefaultBaseURL + `)
  --token <tok>    required per-session X-FragForge-Token
                   (default $ZV_MUTATION_TOKEN)

Keys (the mouse works too: click tabs and rows, wheel to scroll, click the
selected row to run its next step):
  ↑/↓ or j/k  navigate      tab  switch Demos / Stream Clips
  u           upload        enter  run the next step for the selected job
  r/c/R       record / compose / render     d  download the composed MP4
  q           quit

Recording and rendering run only where capture is configured (a Windows+GPU
host with HLAE/CS2/ffmpeg); the header shows what this orchestrator supports.
`

func main() {
	fs := flag.NewFlagSet("zv-tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	url := fs.String("url", "", "orchestrator base URL")
	token := fs.String("token", "", "X-FragForge-Token")
	help := fs.Bool("help", false, "show help")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *help {
		fmt.Fprint(os.Stdout, usage)
		return
	}

	cl := tuiclient.New(tuiclient.Config{BaseURL: *url, Token: *token})
	p := tea.NewProgram(newModel(cl, fs.Args()), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zv-tui: %v\n", err)
		os.Exit(1)
	}
}
