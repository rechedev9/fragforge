import assert from 'node:assert/strict';
import test from 'node:test';
import { terminateNodeAppServerProcess, windowsCommandLine } from './app-server-transport.ts';

test('serializes the Studio app-server config argv through the Windows npm cmd shim', () => {
  const command = windowsCommandLine('codex', [
    'app-server',
    '--config', "web_search='disabled'",
    '--config', 'mcp_servers={}',
    '--strict-config',
  ]);

  assert.equal(command, "codex app-server --config web_search='disabled' --config mcp_servers={} --strict-config");
  assert.throws(() => windowsCommandLine('codex', ['app-server', '--config', 'web_search="disabled"']), /unsupported Windows shell syntax/);
});

test('terminates the full Windows command tree for an npm cmd shim', () => {
  let killed = false;
  const taskkillPids: number[] = [];
  terminateNodeAppServerProcess(
    {
      kill: () => {
        killed = true;
        return true;
      },
      pid: 4242,
    },
    'win32',
    (pid) => {
      taskkillPids.push(pid);
      return { status: 0 };
    },
  );
  assert.deepEqual(taskkillPids, [4242]);
  assert.equal(killed, false, 'taskkill /T owns descendant termination');
});

test('falls back to direct kill and reports a failed Windows tree termination', () => {
  let killed = false;
  assert.throws(() => terminateNodeAppServerProcess(
    {
      kill: () => {
        killed = true;
        return true;
      },
      pid: 99,
    },
    'win32',
    () => ({ status: 1, stderr: 'access denied' }),
  ), /taskkill exited 1: access denied/);
  assert.equal(killed, true);
});

test('uses direct child termination outside Windows', () => {
  let killed = false;
  terminateNodeAppServerProcess({
    kill: () => {
      killed = true;
      return true;
    },
    pid: 7,
  }, 'linux');
  assert.equal(killed, true);
});
