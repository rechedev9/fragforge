/**
 * Narrow bridge exposed only by the FragForge Studio Electron preload.
 *
 * The browser UI deliberately has no HTTP fallback for these operations: the
 * xAI credential must go straight to the desktop main process so it never
 * reaches Next.js, the orchestrator API, localStorage, or a URL.
 */

export const XAI_KEY_SOURCES = {
  environment: 'environment',
  stored: 'stored',
  team: 'team',
  none: 'none',
} as const;

export type XAIKeySource = typeof XAI_KEY_SOURCES[keyof typeof XAI_KEY_SOURCES];

export type XAISettingsStatus = {
  storageAvailable: boolean;
  stored: boolean;
  active: boolean;
  activeSource: XAIKeySource;
  pendingSource: XAIKeySource;
  restartRequired: boolean;
  storageError?: string;
};

export type XAISettingsMutationResult = {
  ok: boolean;
  status?: XAISettingsStatus;
  error?: string;
};

export type XAIConnectionTestResult = {
  ok: boolean;
  code: string;
  message: string;
};

export type StudioRestartResult = {
  ok: boolean;
  error?: string;
};

/**
 * Registration material for the local MCP server, computed by the Electron
 * main process so the launcher path always matches this machine's install.
 */
export type MCPConfigInfo = {
  launcherPath: string;
  launcherInstalled: boolean;
  claudeCommand: string;
  mcpServersJSON: string;
};

export interface DesktopSettingsBridge {
  getXAIStatus(): Promise<XAISettingsStatus>;
  saveXAIKey(apiKey: string): Promise<XAISettingsMutationResult>;
  removeXAIKey(): Promise<XAISettingsMutationResult>;
  testXAIKey(apiKey: string): Promise<XAIConnectionTestResult>;
  restartStudio(): Promise<StudioRestartResult>;
  getMCPConfig(): Promise<MCPConfigInfo>;
}

/**
 * Returns the preload bridge when running inside FragForge Studio. A normal
 * browser (including frontend-only development) receives null and must render
 * the desktop-only state instead of attempting a network fallback.
 */
export function getDesktopSettingsBridge(scope: unknown = globalThis): DesktopSettingsBridge | null {
  if (!isRecord(scope)) return null;
  const candidate = scope.fragforgeSettings;
  return isDesktopSettingsBridge(candidate) ? candidate : null;
}

function isDesktopSettingsBridge(value: unknown): value is DesktopSettingsBridge {
  if (!isRecord(value)) return false;
  return typeof value.getXAIStatus === 'function'
    && typeof value.saveXAIKey === 'function'
    && typeof value.removeXAIKey === 'function'
    && typeof value.testXAIKey === 'function'
    && typeof value.restartStudio === 'function'
    && typeof value.getMCPConfig === 'function';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
