import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { installBundledHLAEPatch, type HLAEPatchSpec } from './hlae-patch.ts';

function digest(value: Buffer): string {
  return createHash('sha256').update(value).digest('hex');
}

function fixture(): {
  root: string;
  hlaeExe: string;
  hook: string;
  bundledHook: string;
  patch: HLAEPatchSpec;
} {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-hlae-patch-'));
  const install = path.join(root, 'install');
  const bundle = path.join(root, 'bundle');
  const hookDir = path.join(install, 'x64');
  fs.mkdirSync(hookDir, { recursive: true });
  fs.mkdirSync(bundle, { recursive: true });
  const hlaeExe = path.join(install, 'HLAE.exe');
  const hook = path.join(hookDir, 'AfxHookSource2.dll');
  const bundledHook = path.join(bundle, 'AfxHookSource2.dll');
  const patched = Buffer.from('verified patched hook');
  fs.writeFileSync(hlaeExe, 'hlae');
  fs.writeFileSync(hook, 'official hook');
  fs.writeFileSync(bundledHook, patched);
  return {
    root,
    hlaeExe,
    hook,
    bundledHook,
    patch: { version: '2.190.2', sha256: digest(patched) },
  };
}

test('installs the verified hook and preserves the official hook once', (t) => {
  const value = fixture();
  t.after(() => fs.rmSync(value.root, { recursive: true, force: true }));

  assert.equal(
    installBundledHLAEPatch(value.hlaeExe, value.patch.version, path.dirname(value.bundledHook), value.patch),
    'installed',
  );
  assert.equal(fs.readFileSync(value.hook, 'utf8'), 'verified patched hook');
  assert.equal(
    fs.readFileSync(path.join(path.dirname(value.hook), 'AfxHookSource2.official-2.190.2.dll'), 'utf8'),
    'official hook',
  );

  assert.equal(
    installBundledHLAEPatch(value.hlaeExe, value.patch.version, path.dirname(value.bundledHook), value.patch),
    'current',
  );
});

test('does not apply a patch built for a different HLAE version', (t) => {
  const value = fixture();
  t.after(() => fs.rmSync(value.root, { recursive: true, force: true }));

  assert.equal(
    installBundledHLAEPatch(value.hlaeExe, '2.190.3', path.dirname(value.bundledHook), value.patch),
    'not-applicable',
  );
  assert.equal(fs.readFileSync(value.hook, 'utf8'), 'official hook');
});

test('rejects a corrupt bundle without changing the official hook', (t) => {
  const value = fixture();
  t.after(() => fs.rmSync(value.root, { recursive: true, force: true }));
  fs.writeFileSync(value.bundledHook, 'corrupt');

  assert.throws(
    () => installBundledHLAEPatch(value.hlaeExe, value.patch.version, path.dirname(value.bundledHook), value.patch),
    /bundled hook sha256 mismatch/,
  );
  assert.equal(fs.readFileSync(value.hook, 'utf8'), 'official hook');
});
