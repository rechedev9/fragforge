# `zv mcp` - MCP server for coding agents

`zv mcp` starts a Model Context Protocol server over stdio that exposes the FragForge demo-to-Short pipeline as typed tools.
It is a thin client of the orchestrator HTTP API, the same surface FragForge Studio and `zv tui` drive, so an agent running on the same machine as the desktop app can take a demo from upload to a finished vertical Short.

## Registering the server

The primary target is a coding agent running on the same Windows PC as FragForge Studio.
Discovery finds the running app by itself, so registration is one line.

### Claude Code (`.mcp.json`)

```json
{
  "mcpServers": {
    "fragforge": {
      "command": "zv",
      "args": ["mcp"]
    }
  }
}
```

### Codex (`~/.codex/config.toml`)

```toml
[mcp_servers.fragforge]
command = "zv"
args = ["mcp"]
```

## Windows setup

FragForge Studio ships `zv-orchestrator.exe` but not `zv.exe`, and the agent needs `zv.exe` to run `zv mcp`.
For now, download `zv.exe` from the GitHub release and drop it next to the agent config (or anywhere on `PATH`), then register it as above.
A later change bundles `zv.exe` into the FragForge Studio install directory next to `zv-orchestrator.exe`, so the registration can point at a well-known path instead.

## Connection discovery

The orchestrator base URL is resolved in this order, first match wins:

1. `--url <addr>`.
2. `$ORCHESTRATOR_URL`.
3. The running FragForge Studio desktop app: its per-install `ports.json` in the Electron `userData` directory (`%APPDATA%\fragforge-studio\ports.json` on Windows).
4. `http://127.0.0.1:8080`, the default `zv serve` bind for local development.

The server always starts, even when the orchestrator is not reachable yet.
MCP clients launch it eagerly, so every tool re-checks health and returns `orchestrator_unavailable` (retryable) until FragForge Studio or `zv serve` is up.

## Developing against `zv serve`

Run the orchestrator in memory mode and point the server at it:

```bash
ZV_DATABASE_URL=memory ZV_DATA_DIR=./data zv serve
zv mcp --url http://127.0.0.1:8080
```

`--token` (or `$ZV_MUTATION_TOKEN`) is forwarded as `X-FragForge-Token` when set, for a non-loopback orchestrator.

## Capture-only tools

Recording and rendering require a capture-configured Windows + GPU host with HLAE, CS2, and FFmpeg.
On any other host `get_capabilities` reports those stages disabled, and `start_recording` / `start_render` fail fast with `capability_missing` before touching the API.
The parse and roster-scan tools (`create_job`, `get_roster`, `start_parse`, `get_plan`, `get_moments`) work anywhere the orchestrator runs.
