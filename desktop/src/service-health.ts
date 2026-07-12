import { waitForHttp } from './http-health.ts';

export type HttpWaiter = (url: string, timeoutMs: number, signal: AbortSignal) => Promise<void>;

export interface DesktopServiceHealthOptions {
  orchestratorUrl: string;
  webUrl: string;
  timeoutMs: number;
  signal: AbortSignal;
  childExited: Promise<never>;
  wait?: HttpWaiter;
}

/** Waits for both desktop services while treating either child exit as terminal. */
export async function waitForDesktopServices(options: DesktopServiceHealthOptions): Promise<void> {
  const wait = options.wait ?? waitForHttp;
  await Promise.race([
    wait(`${options.orchestratorUrl}/healthz`, options.timeoutMs, options.signal),
    options.childExited,
  ]);
  await Promise.race([
    wait(options.webUrl, options.timeoutMs, options.signal),
    options.childExited,
  ]);
}
