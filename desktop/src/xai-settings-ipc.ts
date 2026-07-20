export const XAI_SETTINGS_CHANNEL = 'fragforge:xai-settings';
const MAX_XAI_API_KEY_INPUT_LENGTH = 4096;

export const XAI_SETTINGS_ACTION = {
  appInfo: 'app-info',
  remove: 'remove',
  restart: 'restart',
  save: 'save',
  status: 'status',
  test: 'test',
} as const;

export type XAISettingsAction = typeof XAI_SETTINGS_ACTION[keyof typeof XAI_SETTINGS_ACTION];
export type XAISettingsSource = 'environment' | 'stored' | 'none';

export type XAISettingsRequest =
  | { action: typeof XAI_SETTINGS_ACTION.appInfo }
  | { action: typeof XAI_SETTINGS_ACTION.status }
  | { action: typeof XAI_SETTINGS_ACTION.save; apiKey: string }
  | { action: typeof XAI_SETTINGS_ACTION.test; apiKey: string }
  | { action: typeof XAI_SETTINGS_ACTION.remove }
  | { action: typeof XAI_SETTINGS_ACTION.restart };

export interface XAISettingsStatus {
  storageAvailable: boolean;
  stored: boolean;
  active: boolean;
  activeSource: XAISettingsSource;
  pendingSource: XAISettingsSource;
  restartRequired: boolean;
  storageError?: string;
}

export interface XAISettingsMutationResult {
  ok: boolean;
  error?: string;
  status?: XAISettingsStatus;
}

export interface XAISettingsRestartResult {
  ok: boolean;
  error?: string;
}

export interface TrustedSettingsSenderInput {
  expectedOrigin: string | null;
  expectedWebContentsID: number | null;
  isMainFrame: boolean;
  senderURL: string;
  senderWebContentsID: number;
}

/** Parses the only messages accepted from the sandboxed preload bridge. */
export function parseXAISettingsRequest(value: unknown): XAISettingsRequest {
  if (!isRecord(value) || !Object.hasOwn(value, 'action') || typeof value.action !== 'string') {
    throw new Error('invalid xAI settings request');
  }
  const action = value.action;
  if (action === XAI_SETTINGS_ACTION.appInfo
    || action === XAI_SETTINGS_ACTION.status
    || action === XAI_SETTINGS_ACTION.remove
    || action === XAI_SETTINGS_ACTION.restart) {
    requireExactKeys(value, ['action']);
    return { action };
  }
  if (action === XAI_SETTINGS_ACTION.save || action === XAI_SETTINGS_ACTION.test) {
    requireExactKeys(value, ['action', 'apiKey']);
    if (typeof value.apiKey !== 'string' || value.apiKey.length > MAX_XAI_API_KEY_INPUT_LENGTH) {
      throw new Error('invalid xAI settings request');
    }
    return { action, apiKey: value.apiKey };
  }
  throw new Error('invalid xAI settings request');
}

/** Accepts IPC only from the active Studio page's top frame and exact web origin. */
export function isTrustedSettingsSender(input: TrustedSettingsSenderInput): boolean {
  if (input.expectedOrigin === null
    || input.expectedWebContentsID === null
    || input.senderWebContentsID !== input.expectedWebContentsID
    || !input.isMainFrame) return false;
  try {
    return new URL(input.senderURL).origin === input.expectedOrigin;
  } catch {
    return false;
  }
}

function requireExactKeys(value: Record<string, unknown>, expected: string[]): void {
  const keys = Object.keys(value).sort();
  const wanted = [...expected].sort();
  if (keys.length !== wanted.length || keys.some((key, index) => key !== wanted[index])) {
    throw new Error('invalid xAI settings request');
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
