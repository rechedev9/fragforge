import { normalizeXAIAPIKey } from './xai-api-key.ts';
import type { XAIConnectionResult } from './xai-connection.ts';
import {
  XAI_SETTINGS_ACTION,
  type XAISettingsMutationResult,
  type XAISettingsRequest,
  type XAISettingsRestartResult,
  type XAISettingsStatus,
} from './xai-settings-ipc.ts';

export interface XAISettingsKeyStore {
  isAvailable(): Promise<boolean>;
  remove(): Promise<boolean>;
  save(value: string): Promise<void>;
}

interface XAISettingsControllerOptions {
  environmentOverride: boolean;
  keyStore: XAISettingsKeyStore;
  readStatus: (restartRequired: boolean) => Promise<XAISettingsStatus>;
  scheduleRestart: () => boolean;
  testConnection: (apiKey: string) => Promise<XAIConnectionResult>;
}

/** Owns the state transition between a saved credential and the next Studio boot. */
export class XAISettingsController {
  readonly #environmentOverride: boolean;
  readonly #keyStore: XAISettingsKeyStore;
  readonly #readStatus: (restartRequired: boolean) => Promise<XAISettingsStatus>;
  readonly #scheduleRestart: () => boolean;
  readonly #testConnection: (apiKey: string) => Promise<XAIConnectionResult>;
  #restartRequired = false;

  constructor(options: XAISettingsControllerOptions) {
    this.#environmentOverride = options.environmentOverride;
    this.#keyStore = options.keyStore;
    this.#readStatus = options.readStatus;
    this.#scheduleRestart = options.scheduleRestart;
    this.#testConnection = options.testConnection;
  }

  get restartRequired(): boolean {
    return this.#restartRequired;
  }

  markApplied(): void {
    this.#restartRequired = false;
  }

  async handle(request: XAISettingsRequest): Promise<unknown> {
    if (request.action === XAI_SETTINGS_ACTION.status) return this.#status();
    if (request.action === XAI_SETTINGS_ACTION.test) return this.#testConnection(request.apiKey);
    if (request.action === XAI_SETTINGS_ACTION.save) return this.#save(request.apiKey);
    if (request.action === XAI_SETTINGS_ACTION.remove) return this.#remove();
    return this.#restart();
  }

  async #save(value: string): Promise<XAISettingsMutationResult> {
    if (this.#environmentOverride) {
      return failure('XAI_API_KEY está definida en el entorno y tiene prioridad. Elimínala antes de guardar una clave desde Ajustes.');
    }
    let normalized: string;
    try {
      normalized = normalizeXAIAPIKey(value);
    } catch {
      return failure('La clave debe ser una sola línea no vacía de hasta 4096 bytes.');
    }
    if (!(await this.#keyStore.isAvailable())) {
      return failure('La protección segura de Windows no está disponible; la clave no se ha guardado.');
    }
    try {
      await this.#keyStore.save(normalized);
    } catch {
      return failure('No se pudo guardar la clave con la protección segura de Windows.');
    }
    this.#restartRequired = true;
    return this.#mutationSuccess();
  }

  async #remove(): Promise<XAISettingsMutationResult> {
    let removed: boolean;
    try {
      removed = await this.#keyStore.remove();
    } catch {
      return failure('No se pudo eliminar la clave guardada.');
    }
    if (removed && !this.#environmentOverride) this.#restartRequired = true;
    return this.#mutationSuccess();
  }

  #restart(): XAISettingsRestartResult {
    if (!this.#restartRequired) return { error: 'No hay cambios de xAI pendientes de aplicar.', ok: false };
    return this.#scheduleRestart()
      ? { ok: true }
      : { error: 'No se pudo programar el reinicio de FragForge Studio.', ok: false };
  }

  #status(): Promise<XAISettingsStatus> {
    return this.#readStatus(this.#restartRequired);
  }

  async #mutationSuccess(): Promise<XAISettingsMutationResult> {
    try {
      return success(await this.#status());
    } catch {
      // The durable mutation already succeeded. Let the renderer retry the
      // independent status read instead of falsely claiming the key change failed.
      return { ok: true };
    }
  }
}

function success(status: XAISettingsStatus): XAISettingsMutationResult {
  return { ok: true, status };
}

function failure(error: string): XAISettingsMutationResult {
  return { error, ok: false };
}
