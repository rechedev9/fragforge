// Relative .ts import (not the `@/` alias) so Node's native TS loader can resolve
// it when pcStatus.test.ts runs this module directly under node --test.
import type { PcStatus } from '../api/dataplane.ts';

/** The `agents` columns /api/pc/status reads to report pairing + liveness. */
export type AgentRow = {
  last_heartbeat_at: string | null;
  loopback_token: string | null;
  loopback_port: number | null;
} | null | undefined;

/** An agent is online when it heartbeat within this window. */
const HEARTBEAT_WINDOW_MS = 60_000;

/**
 * Builds the browser-facing /api/pc/status body from the newest agent row. A
 * missing row is unpaired. `loopback` is non-null only when the agent has
 * reported a token (an empty token means paired-but-not-yet-configured, which
 * the client cannot dial), so the browser only ever learns a usable endpoint.
 */
export function pcStatus(agent: AgentRow, now: number = Date.now()): PcStatus {
  if (!agent) return { paired: false, online: false, loopback: null };
  const online = !!agent.last_heartbeat_at && now - new Date(agent.last_heartbeat_at).getTime() < HEARTBEAT_WINDOW_MS;
  const loopback = agent.loopback_token
    ? { port: agent.loopback_port ?? 8090, token: agent.loopback_token }
    : null;
  return { paired: true, online, loopback };
}
