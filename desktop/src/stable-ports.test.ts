import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as net from 'node:net';
import * as os from 'node:os';
import * as path from 'node:path';
import { allocateStableServicePorts } from './stable-ports.ts';

const TEST_HOST = '127.0.0.1';

function temporaryPortsFile(t: test.TestContext): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-ports-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return path.join(root, 'ports.json');
}

function portSequence(ports: number[]): (host: string) => Promise<number> {
  let index = 0;
  return async (_host) => {
    const port = ports[index];
    index += 1;
    if (port === undefined) throw new Error('test port sequence exhausted');
    return port;
  };
}

test('reuses two valid saved ports without rewriting the file', async (t) => {
  const portsFile = temporaryPortsFile(t);
  const original = '{\n  "orchestrator": 41001,\n  "web": 42002,\n  "keep": true\n}\n';
  fs.writeFileSync(portsFile, original);
  const probes: number[] = [];

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: () => {},
    isPortFree: async (port) => {
      probes.push(port);
      return true;
    },
    allocateFreePort: async () => {
      throw new Error('unexpected allocation');
    },
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 42002 });
  assert.deepEqual(probes, [41001, 42002]);
  assert.equal(fs.readFileSync(portsFile, 'utf8'), original);
});

test('allocates and persists a distinct pair when no file exists', async (t) => {
  const portsFile = temporaryPortsFile(t);
  let probes = 0;

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: () => {},
    isPortFree: async () => {
      probes += 1;
      return true;
    },
    allocateFreePort: portSequence([41001, 42002]),
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 42002 });
  assert.equal(probes, 0);
  assert.deepEqual(JSON.parse(fs.readFileSync(portsFile, 'utf8')), ports);
  assert.equal(fs.existsSync(`${portsFile}.tmp`), false);
});

test('retries when the free-port allocator repeats the first service port', async (t) => {
  const portsFile = temporaryPortsFile(t);

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: () => {},
    allocateFreePort: portSequence([41001, 41001, 42002]),
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 42002 });
});

test('rejects an invalid port returned by the allocator', async (t) => {
  const portsFile = temporaryPortsFile(t);

  await assert.rejects(
    allocateStableServicePorts({
      host: TEST_HOST,
      portsFile,
      logLine: () => {},
      allocateFreePort: async () => 0,
    }),
    /free port allocator returned invalid port 0/,
  );

  assert.equal(fs.existsSync(portsFile), false);
});

test('bounds retries when the allocator cannot produce a distinct pair', async (t) => {
  const portsFile = temporaryPortsFile(t);
  let allocations = 0;

  await assert.rejects(
    allocateStableServicePorts({
      host: TEST_HOST,
      portsFile,
      logLine: () => {},
      allocateFreePort: async () => {
        allocations += 1;
        return 41001;
      },
    }),
    /could not allocate distinct desktop service ports/,
  );

  assert.equal(allocations, 33);
  assert.equal(fs.existsSync(portsFile), false);
});

test('repairs duplicate saved ports instead of assigning both children one port', async (t) => {
  const portsFile = temporaryPortsFile(t);
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: 41001, web: 41001 }));
  const probes: number[] = [];
  const logs: string[] = [];

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: (line) => logs.push(line),
    isPortFree: async (port) => {
      probes.push(port);
      return true;
    },
    allocateFreePort: portSequence([42002]),
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 42002 });
  assert.deepEqual(probes, [41001]);
  assert.deepEqual(logs, [
    '[ports] saved web port 41001 conflicts with another service, picking a new one; the reel library kept in the browser localStorage is keyed by origin, so it may appear empty on the new port\n',
  ]);
  assert.deepEqual(JSON.parse(fs.readFileSync(portsFile, 'utf8')), ports);
});

test('does not let a fresh orchestrator allocation steal the saved web port', async (t) => {
  const portsFile = temporaryPortsFile(t);
  fs.writeFileSync(portsFile, JSON.stringify({ web: 42002 }));

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: () => {},
    isPortFree: async () => true,
    allocateFreePort: portSequence([42002, 41001]),
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 42002 });
});

test('replaces an occupied web port and preserves unknown saved keys', async (t) => {
  const portsFile = temporaryPortsFile(t);
  fs.writeFileSync(portsFile, JSON.stringify({
    orchestrator: 41001,
    web: 42002,
    futureSetting: 'preserve me',
  }));
  const logs: string[] = [];

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: (line) => logs.push(line),
    isPortFree: async (port) => port !== 42002,
    allocateFreePort: portSequence([43003]),
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 43003 });
  assert.deepEqual(logs, [
    '[ports] saved web port 42002 was taken, picking a new one; the reel library kept in the browser localStorage is keyed by origin, so it may appear empty on the new port\n',
  ]);
  assert.deepEqual(JSON.parse(fs.readFileSync(portsFile, 'utf8')), {
    orchestrator: 41001,
    web: 43003,
    futureSetting: 'preserve me',
  });
  assert.equal(fs.existsSync(`${portsFile}.tmp`), false);
});

test('replaces invalid saved values without probing them', async (t) => {
  const invalidPorts: unknown[] = [0, -1, 1.5, '41001', 65_536];
  for (const [index, invalidPort] of invalidPorts.entries()) {
    const portsFile = path.join(path.dirname(temporaryPortsFile(t)), `ports-${index}.json`);
    fs.writeFileSync(portsFile, JSON.stringify({
      orchestrator: invalidPort,
      web: invalidPort,
      keep: index,
    }));
    let probes = 0;

    const ports = await allocateStableServicePorts({
      host: TEST_HOST,
      portsFile,
      logLine: () => {},
      isPortFree: async () => {
        probes += 1;
        return true;
      },
      allocateFreePort: portSequence([41001 + index, 42002 + index]),
    });

    assert.deepEqual(ports, { orchestrator: 41001 + index, web: 42002 + index });
    assert.equal(probes, 0);
    assert.deepEqual(JSON.parse(fs.readFileSync(portsFile, 'utf8')), {
      orchestrator: 41001 + index,
      web: 42002 + index,
      keep: index,
    });
  }
});

test('regenerates malformed and non-object port files', async (t) => {
  const invalidDocuments = ['{', '[]', 'null'];
  for (const [index, document] of invalidDocuments.entries()) {
    const portsFile = path.join(path.dirname(temporaryPortsFile(t)), `invalid-${index}.json`);
    fs.writeFileSync(portsFile, document);

    const ports = await allocateStableServicePorts({
      host: TEST_HOST,
      portsFile,
      logLine: () => {},
      allocateFreePort: portSequence([41001 + index, 42002 + index]),
    });

    assert.deepEqual(JSON.parse(fs.readFileSync(portsFile, 'utf8')), ports);
  }
});

test('returns usable ports when persistence fails', async (t) => {
  const root = path.dirname(temporaryPortsFile(t));
  const portsFile = path.join(root, 'missing', 'ports.json');
  const logs: string[] = [];

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: (line) => logs.push(line),
    allocateFreePort: portSequence([41001, 42002]),
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 42002 });
  assert.equal(logs.length, 1);
  assert.match(logs[0] ?? '', /^\[ports\] could not persist service ports:/);
});

test('cleans the staged file when atomic replacement fails', async (t) => {
  const portsFile = temporaryPortsFile(t);
  fs.mkdirSync(portsFile);
  const logs: string[] = [];

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: (line) => logs.push(line),
    allocateFreePort: portSequence([41001, 42002]),
  });

  assert.deepEqual(ports, { orchestrator: 41001, web: 42002 });
  assert.equal(fs.statSync(portsFile).isDirectory(), true);
  assert.equal(fs.existsSync(`${portsFile}.tmp`), false);
  assert.equal(logs.length, 1);
  assert.match(logs[0] ?? '', /^\[ports\] could not persist service ports:/);
});

test('cancellation between saved-port probes stops resolution', async (t) => {
  const portsFile = temporaryPortsFile(t);
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: 41001, web: 42002 }));
  const controller = new AbortController();
  const probes: number[] = [];

  await assert.rejects(
    allocateStableServicePorts({
      host: TEST_HOST,
      portsFile,
      signal: controller.signal,
      logLine: () => {},
      isPortFree: async (port) => {
        probes.push(port);
        controller.abort();
        return true;
      },
      allocateFreePort: async () => {
        throw new Error('unexpected allocation');
      },
    }),
    /port allocation aborted/,
  );

  assert.deepEqual(probes, [41001]);
});

test('cancellation during allocation stops before persistence or another service', async (t) => {
  const portsFile = temporaryPortsFile(t);
  const controller = new AbortController();
  let allocations = 0;

  await assert.rejects(
    allocateStableServicePorts({
      host: TEST_HOST,
      portsFile,
      signal: controller.signal,
      logLine: () => {},
      allocateFreePort: async () => {
        allocations += 1;
        controller.abort();
        return 41001;
      },
    }),
    /port allocation aborted/,
  );

  assert.equal(allocations, 1);
  assert.equal(fs.existsSync(portsFile), false);
});

test('detects a port occupied by a live loopback server', async (t) => {
  const portsFile = temporaryPortsFile(t);
  const server = net.createServer();
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, TEST_HOST, resolve);
  });
  t.after(() => new Promise<void>((resolve, reject) => {
    server.close((err) => {
      if (err) reject(err);
      else resolve();
    });
  }));
  const address = server.address();
  if (address === null || typeof address === 'string') {
    throw new Error('test server has no TCP address');
  }
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: address.port }));
  const logs: string[] = [];

  const ports = await allocateStableServicePorts({
    host: TEST_HOST,
    portsFile,
    logLine: (line) => logs.push(line),
  });

  assert.notEqual(ports.orchestrator, address.port);
  assert.notEqual(ports.orchestrator, ports.web);
  assert.equal(
    logs[0],
    `[ports] saved orchestrator port ${address.port} was taken, picking a new one;\n`,
  );
});
