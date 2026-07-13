import { readFileSync } from 'node:fs';
import * as path from 'node:path';
import { JsonRpcProtocolError } from './json-rpc.ts';
import { isJsonObject } from './json.ts';
import { runFragForgeMcp } from './runtime.ts';

runFragForgeMcp({
  diagnostics: process.stderr,
  input: process.stdin,
  onClose: (reason) => {
    if (reason instanceof JsonRpcProtocolError) process.exitCode = 1;
    process.stdin.destroy();
  },
  output: process.stdout,
  serverVersion: serverVersion(),
});

function serverVersion(): string {
  const entry = process.argv[1];
  if (entry === undefined) return 'development';
  try {
    const packagePath = path.resolve(path.dirname(entry), '..', '..', 'package.json');
    const document: unknown = JSON.parse(readFileSync(packagePath, 'utf8'));
    return isJsonObject(document) && typeof document.version === 'string'
      ? document.version
      : 'development';
  } catch {
    return 'development';
  }
}
