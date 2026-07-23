import assert from 'node:assert/strict';
import { existsSync, mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { pruneSharpPlatforms } from './standalone-runtime.mjs';

test('keeps only Windows x64 Sharp runtime packages in the desktop bundle', () => {
  const directory = mkdtempSync(join(tmpdir(), 'fragforge-standalone-runtime-'));
  const imagePackages = join(directory, '@img');
  try {
    for (const name of [
      'colour',
      'sharp-darwin-x64',
      'sharp-libvips-linux-x64',
      'sharp-win32-arm64',
      'sharp-win32-x64',
    ]) {
      mkdirSync(join(imagePackages, name), { recursive: true });
    }

    pruneSharpPlatforms(directory);

    assert.equal(existsSync(join(imagePackages, 'colour')), true);
    assert.equal(existsSync(join(imagePackages, 'sharp-win32-x64')), true);
    assert.equal(existsSync(join(imagePackages, 'sharp-darwin-x64')), false);
    assert.equal(existsSync(join(imagePackages, 'sharp-libvips-linux-x64')), false);
    assert.equal(existsSync(join(imagePackages, 'sharp-win32-arm64')), false);
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test('accepts a standalone tree without optional Sharp packages', () => {
  const directory = mkdtempSync(join(tmpdir(), 'fragforge-standalone-runtime-empty-'));
  try {
    assert.doesNotThrow(() => pruneSharpPlatforms(directory));
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});
