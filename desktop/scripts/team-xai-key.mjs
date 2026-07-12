import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const MAX_XAI_API_KEY_BYTES = 4096;
const INVALID_TEAM_KEY_MESSAGE =
  'team build requires XAI_API_KEY to contain a single non-empty line no longer than 4096 bytes';
const TEAM_FLAG = '--team-xai-key';

/** Validates the public CLI shape without ever echoing an unsupported value. */
export function assembleUsesTeamXAIKey(args) {
  if (args.some((arg) => arg !== TEAM_FLAG) || args.filter((arg) => arg === TEAM_FLAG).length > 1) {
    throw new Error('unsupported assemble argument');
  }
  return args.includes(TEAM_FLAG);
}

/** Selects the credential only for the explicit internal team build. */
export function resolveTeamXAIKey(enabled, environment = process.env) {
  if (!enabled) return '';
  const value = typeof environment.XAI_API_KEY === 'string'
    ? environment.XAI_API_KEY.trim()
    : '';
  if (
    !value
    || value.includes('\n')
    || value.includes('\r')
    || value.includes('\0')
    || Buffer.byteLength(value, 'utf8') > MAX_XAI_API_KEY_BYTES
  ) {
    throw new Error(INVALID_TEAM_KEY_MESSAGE);
  }
  return value;
}

/** Writes one deterministic resource; an empty normal build erases stale key material. */
export function stageTeamXAIKey(teamDirectory, value) {
  mkdirSync(teamDirectory, { recursive: true });
  writeFileSync(join(teamDirectory, 'xai-api-key'), value, { encoding: 'utf8', mode: 0o600 });
}

/** Copies an environment while dropping every casing variant of the credential. */
export function environmentWithoutXAIAPIKey(environment = process.env) {
  const sanitized = { ...environment };
  for (const name of Object.keys(sanitized)) {
    if (name.toLowerCase() === 'xai_api_key') delete sanitized[name];
  }
  return sanitized;
}
