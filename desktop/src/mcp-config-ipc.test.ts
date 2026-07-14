import assert from 'node:assert/strict';
import test from 'node:test';
import { buildMCPConfigInfo, parseMCPConfigRequest } from './mcp-config-ipc.ts';

test('parseMCPConfigRequest accepts only the exact info action', () => {
  assert.deepEqual(parseMCPConfigRequest({ action: 'info' }), { action: 'info' });
  for (const invalid of [null, 'info', { action: 'status' }, { action: 'info', extra: 1 }, {}]) {
    assert.throws(() => parseMCPConfigRequest(invalid), /invalid MCP config request/);
  }
});

test('buildMCPConfigInfo embeds the launcher path in both snippets', () => {
  const launcherPath = 'C:\\Users\\luis\\AppData\\Local\\Programs\\FragForge Studio\\fragforge-mcp.cmd';

  const info = buildMCPConfigInfo(launcherPath, true);

  assert.equal(info.launcherPath, launcherPath);
  assert.equal(info.launcherInstalled, true);
  assert.equal(
    info.claudeCommand,
    `claude mcp add --transport stdio --scope user fragforge -- cmd.exe /d /s /c "${launcherPath}"`,
  );
  const parsed = JSON.parse(info.mcpServersJSON) as {
    mcpServers: { fragforge: { command: string; args: string[] } };
  };
  assert.equal(parsed.mcpServers.fragforge.command, 'cmd.exe');
  assert.deepEqual(parsed.mcpServers.fragforge.args, ['/d', '/s', '/c', launcherPath]);
});
