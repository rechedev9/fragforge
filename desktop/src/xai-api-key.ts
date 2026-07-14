import * as fs from 'node:fs';

const MAX_XAI_API_KEY_BYTES = 4096;
const INVALID_KEY_MESSAGE =
  'XAI_API_KEY must contain a single non-empty line no longer than 4096 bytes';

export type XAIAPIKeySource = 'environment' | 'stored' | 'team' | 'none';

export interface ResolveXAIAPIKeyOptions {
  environmentValue?: string;
  storedValue?: string;
  bundledPath: string;
  readFile?: (filePath: string) => string;
}

export interface ResolvedXAIAPIKey {
  apiKey?: string;
  source: XAIAPIKeySource;
}

/**
 * Resolves the subtitle credential for the orchestrator. A local environment
 * value intentionally wins for emergency rotation. A key stored securely for
 * the current OS user wins over the optional internal-team bundle.
 */
export function resolveXAIAPIKeyDetails(options: ResolveXAIAPIKeyOptions): ResolvedXAIAPIKey {
  if (hasValue(options.environmentValue)) {
    return {
      apiKey: normalizeXAIAPIKey(options.environmentValue),
      source: 'environment',
    };
  }
  if (hasValue(options.storedValue)) {
    return {
      apiKey: normalizeXAIAPIKey(options.storedValue),
      source: 'stored',
    };
  }

  let bundledValue: string;
  try {
    bundledValue = (options.readFile ?? readUTF8File)(options.bundledPath);
  } catch (err) {
    if (isMissingFileError(err)) return { source: 'none' };
    const detail = err instanceof Error ? err.message : String(err);
    throw new Error(`could not read the packaged team xAI API key: ${detail}`);
  }
  if (!hasValue(bundledValue)) return { source: 'none' };
  return {
    apiKey: normalizeXAIAPIKey(bundledValue),
    source: 'team',
  };
}

/** Backwards-compatible value-only view used by the desktop boot path. */
export function resolveXAIAPIKey(options: ResolveXAIAPIKeyOptions): string | undefined {
  return resolveXAIAPIKeyDetails(options).apiKey;
}

/**
 * Trims copy/paste whitespace and rejects values that could escape an HTTP
 * Authorization header or exceed the deliberately small credential bound.
 */
export function normalizeXAIAPIKey(value: string): string {
  const normalized = value.trim();
  if (
    normalized.length === 0
    || normalized.includes('\n')
    || normalized.includes('\r')
    || normalized.includes('\0')
    || Buffer.byteLength(normalized, 'utf8') > MAX_XAI_API_KEY_BYTES
  ) {
    throw new Error(INVALID_KEY_MESSAGE);
  }
  return normalized;
}

/**
 * Captures the inherited credential once and removes every casing variant so
 * unrelated children cannot inherit it later. The canonical uppercase name is
 * preferred if a synthetic environment contains duplicate casing variants.
 */
export function takeXAIAPIKeyFromEnvironment(
  environment: NodeJS.ProcessEnv = process.env,
): string | undefined {
  const matchingNames = Object.keys(environment).filter(
    (name) => name.toLowerCase() === 'xai_api_key',
  );
  const canonicalName = matchingNames.find((name) => name === 'XAI_API_KEY');
  const selectedName = canonicalName ?? matchingNames[0];
  const captured = selectedName === undefined ? undefined : environment[selectedName];
  for (const name of matchingNames) delete environment[name];
  return captured;
}

function readUTF8File(filePath: string): string {
  return fs.readFileSync(filePath, 'utf8');
}

function isMissingFileError(err: unknown): boolean {
  return err instanceof Error && 'code' in err && err.code === 'ENOENT';
}

function hasValue(value: string | undefined): value is string {
  return typeof value === 'string' && value.trim() !== '';
}
