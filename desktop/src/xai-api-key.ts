const MAX_XAI_API_KEY_BYTES = 4096;
const INVALID_KEY_MESSAGE =
  'XAI_API_KEY must contain a single non-empty line no longer than 4096 bytes';

export type XAIAPIKeySource = 'environment' | 'stored' | 'none';

export interface ResolveXAIAPIKeyOptions {
  environmentValue?: string;
  storedValue?: string;
}

export interface ResolvedXAIAPIKey {
  apiKey?: string;
  source: XAIAPIKeySource;
}

/**
 * Resolves the subtitle credential for the orchestrator. A local environment
 * value intentionally wins for emergency rotation, followed by a key stored
 * securely for the current OS user. Desktop builds contain no shared fallback.
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
  return { source: 'none' };
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

function hasValue(value: string | undefined): value is string {
  return typeof value === 'string' && value.trim() !== '';
}
