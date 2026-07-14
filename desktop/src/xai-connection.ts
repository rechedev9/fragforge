import { normalizeXAIAPIKey } from './xai-api-key.ts';

const XAI_API_KEY_STATUS_URL = 'https://api.x.ai/v1/api-key';
const XAI_CONNECTION_TIMEOUT_MS = 10_000;
const XAI_STATUS_RESPONSE_MAX_BYTES = 64 * 1024;

export type XAIConnectionCode =
  | 'valid'
  | 'invalid_format'
  | 'invalid'
  | 'blocked'
  | 'missing_permissions'
  | 'rate_limited'
  | 'network_error'
  | 'service_error';

export interface XAIConnectionResult {
  ok: boolean;
  code: XAIConnectionCode;
  message: string;
}

interface TestXAIConnectionOptions {
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
}

/** Validates an xAI key without sending media or returning any account metadata. */
export async function testXAIConnection(
  value: string,
  options: TestXAIConnectionOptions = {},
): Promise<XAIConnectionResult> {
  let apiKey: string;
  try {
    apiKey = normalizeXAIAPIKey(value.trim());
  } catch {
    return result(false, 'invalid_format', 'La clave debe ser una sola línea no vacía de hasta 4096 bytes.');
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), options.timeoutMs ?? XAI_CONNECTION_TIMEOUT_MS);
  try {
    const response = await (options.fetchImpl ?? fetch)(XAI_API_KEY_STATUS_URL, {
      headers: {
        Accept: 'application/json',
        Authorization: `Bearer ${apiKey}`,
      },
      method: 'GET',
      redirect: 'error',
      signal: controller.signal,
    });

    if (response.status === 400 || response.status === 401 || response.status === 403) {
      await cancelBody(response);
      return result(false, 'invalid', 'xAI rechazó la clave. Comprueba que esté activa y bien copiada.');
    }
    if (response.status === 429) {
      await cancelBody(response);
      return result(false, 'rate_limited', 'xAI reconoce la solicitud, pero la cuenta está limitada temporalmente.');
    }
    if (!response.ok) {
      await cancelBody(response);
      return result(false, 'service_error', 'xAI no pudo comprobar la clave en este momento.');
    }

    const status = await readStatusResponse(response);
    if (status === undefined) {
      return controller.signal.aborted
        ? timeoutResult()
        : result(false, 'service_error', 'xAI devolvió una respuesta de estado no válida.');
    }
    if (status.apiKeyBlocked || status.apiKeyDisabled || status.teamBlocked) {
      return result(false, 'blocked', 'La clave o su equipo están bloqueados o desactivados en xAI.');
    }
    if (status.permissions.length === 0) {
      return result(false, 'missing_permissions', 'La clave es válida, pero no tiene permisos asignados en xAI.');
    }
    return result(true, 'valid', 'Clave válida y activa. Asegúrate de concederle acceso a Speech-to-Text.');
  } catch {
    return controller.signal.aborted
      ? timeoutResult()
      : result(false, 'network_error', 'No se pudo conectar con xAI. Comprueba tu conexión e inténtalo de nuevo.');
  } finally {
    clearTimeout(timeout);
  }
}

interface XAIStatusResponse {
  apiKeyBlocked: boolean;
  apiKeyDisabled: boolean;
  permissions: string[];
  teamBlocked: boolean;
}

async function readStatusResponse(response: Response): Promise<XAIStatusResponse | undefined> {
  const bytes = await readBoundedBody(response, XAI_STATUS_RESPONSE_MAX_BYTES);
  if (bytes === undefined) return undefined;
  let value: unknown;
  try {
    value = JSON.parse(bytes.toString('utf8'));
  } catch {
    return undefined;
  }
  if (!isRecord(value)
    || typeof value.api_key_blocked !== 'boolean'
    || typeof value.api_key_disabled !== 'boolean'
    || typeof value.team_blocked !== 'boolean'
    || !Array.isArray(value.acls)
    || !value.acls.every((permission) => typeof permission === 'string')) return undefined;
  return {
    apiKeyBlocked: value.api_key_blocked,
    apiKeyDisabled: value.api_key_disabled,
    permissions: value.acls,
    teamBlocked: value.team_blocked,
  };
}

async function readBoundedBody(response: Response, maximum: number): Promise<Buffer | undefined> {
  if (response.body === null) return undefined;
  const reader = response.body.getReader();
  const chunks: Buffer[] = [];
  let total = 0;
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) break;
      total += next.value.byteLength;
      if (total > maximum) {
        await reader.cancel();
        return undefined;
      }
      chunks.push(Buffer.from(next.value));
    }
  } catch {
    return undefined;
  } finally {
    reader.releaseLock();
  }
  return Buffer.concat(chunks, total);
}

async function cancelBody(response: Response): Promise<void> {
  try {
    await response.body?.cancel();
  } catch {
    // Status is already authoritative; body cancellation is best effort.
  }
}

function result(ok: boolean, code: XAIConnectionCode, message: string): XAIConnectionResult {
  return { code, message, ok };
}

function timeoutResult(): XAIConnectionResult {
  return result(false, 'network_error', 'La comprobación con xAI agotó el tiempo de espera.');
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
