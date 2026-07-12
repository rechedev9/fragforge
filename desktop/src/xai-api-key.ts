import * as fs from 'node:fs';

const MAX_XAI_API_KEY_BYTES = 4096;
const INVALID_KEY_MESSAGE =
  'XAI_API_KEY must contain a single non-empty line no longer than 4096 bytes';

interface ResolveXAIAPIKeyOptions {
  environmentValue?: string;
  bundledPath: string;
  readFile?: (filePath: string) => string;
}

/**
 * Resolves the subtitle credential for the orchestrator. A local environment
 * value intentionally wins so a packaged team build can be rotated or
 * overridden without reinstalling the application.
 */
export function resolveXAIAPIKey(options: ResolveXAIAPIKeyOptions): string | undefined {
  const environmentValue = options.environmentValue?.trim();
  if (environmentValue) return normalizeXAIAPIKey(environmentValue);

  let bundledValue: string;
  try {
    bundledValue = (options.readFile ?? readUTF8File)(options.bundledPath);
  } catch (err) {
    if (isMissingFileError(err)) return undefined;
    const detail = err instanceof Error ? err.message : String(err);
    throw new Error(`could not read the packaged team xAI API key: ${detail}`);
  }
  const trimmed = bundledValue.trim();
  if (!trimmed) return undefined;
  return normalizeXAIAPIKey(trimmed);
}

function normalizeXAIAPIKey(value: string): string {
  if (
    value.includes('\n')
    || value.includes('\r')
    || value.includes('\0')
    || Buffer.byteLength(value, 'utf8') > MAX_XAI_API_KEY_BYTES
  ) {
    throw new Error(INVALID_KEY_MESSAGE);
  }
  return value;
}

function readUTF8File(filePath: string): string {
  return fs.readFileSync(filePath, 'utf8');
}

function isMissingFileError(err: unknown): boolean {
  return err instanceof Error && 'code' in err && err.code === 'ENOENT';
}
