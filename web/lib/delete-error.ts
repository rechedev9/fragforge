import { SERVICE_UNAVAILABLE_CODE } from './api/types.ts';

/**
 * Shown inline near a Partidas row when a delete fails because the local
 * analysis service is unreachable, matching the page's "offline" hint copy.
 */
export const DELETE_OFFLINE_MESSAGE = 'Servicio de análisis local desconectado. Arráncalo y reintenta.';

/** Fallback when the error carries no usable message (unexpected transport failure). */
export const DELETE_GENERIC_MESSAGE = 'No se pudo borrar. Inténtalo de nuevo.';

/**
 * Maps a delete failure to the Spanish message the row should surface. An
 * offline (service_unavailable) error gets the "start your orchestrator" hint;
 * a 409 (job still processing) carries the orchestrator's own explanation as
 * its message (e.g. "Espera a que termine la captura para borrar"), so we pass
 * that through; anything without a message falls back to a generic retry line.
 * Pure and unit-tested so the button component never branches on error shapes.
 */
export function deleteErrorMessage(err: unknown): string {
  const e = err as { code?: unknown; message?: unknown } | null;
  if (e?.code === SERVICE_UNAVAILABLE_CODE) return DELETE_OFFLINE_MESSAGE;
  if (typeof e?.message === 'string' && e.message.trim() !== '') return e.message;
  return DELETE_GENERIC_MESSAGE;
}
