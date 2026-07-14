export const MCP_CONFIG_CHANNEL = 'fragforge:mcp-config';

export const MCP_CONFIG_ACTION = {
  info: 'info',
} as const;

export type MCPConfigRequest = { action: typeof MCP_CONFIG_ACTION.info };

/**
 * Everything the Ajustes card needs to register the local MCP server: the
 * machine's real launcher path plus ready-to-copy snippets for Claude Code
 * (CLI command) and mcpServers-style clients (JSON). The launcher discovers
 * the running Studio's orchestrator port on its own, so no env is required.
 */
export interface MCPConfigInfo {
  launcherPath: string;
  launcherInstalled: boolean;
  claudeCommand: string;
  mcpServersJSON: string;
}

/** Parses the only message accepted from the sandboxed preload bridge. */
export function parseMCPConfigRequest(value: unknown): MCPConfigRequest {
  if (
    typeof value !== 'object' || value === null
    || !Object.hasOwn(value, 'action')
    || (value as Record<string, unknown>).action !== MCP_CONFIG_ACTION.info
    || Object.keys(value).length !== 1
  ) {
    throw new Error('invalid MCP config request');
  }
  return { action: MCP_CONFIG_ACTION.info };
}

export function buildMCPConfigInfo(launcherPath: string, launcherInstalled: boolean): MCPConfigInfo {
  // Mirrors the registration documented in desktop/README.md: the launcher is
  // a .cmd, so clients must start it through cmd.exe.
  const claudeCommand = `claude mcp add --transport stdio --scope user fragforge -- cmd.exe /d /s /c "${launcherPath}"`;
  const mcpServersJSON = JSON.stringify(
    { mcpServers: { fragforge: { command: 'cmd.exe', args: ['/d', '/s', '/c', launcherPath] } } },
    null,
    2,
  );
  return { claudeCommand, launcherInstalled, launcherPath, mcpServersJSON };
}
