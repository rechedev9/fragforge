'use client';

import { useEffect } from 'react';
import { shouldReloadAfterEviction, SW_EVICTED_RELOAD_KEY } from '@/lib/sw-eviction';

/**
 * Unregisters any service worker registered for this origin, once on mount.
 *
 * FragForge never registers a service worker, so any registration found here is
 * foreign by definition — typically a stale worker left behind by another
 * project that previously served on the same localhost port. Such a worker can
 * silently intercept and hang <video> media requests (readyState stuck at 0) in
 * Local Studio, so we proactively evict it.
 *
 * Unregistering does not release a page the worker already controls, so when
 * this page was controlled and something was evicted, the component reloads
 * once (guarded per-tab via sessionStorage) so the eviction takes effect on the
 * current visit instead of the next one. Fully defensive: on any failure it
 * no-ops rather than breaking the app.
 */
export function ServiceWorkerCleanup(): null {
  useEffect(() => {
    if (!('serviceWorker' in navigator)) return;
    void (async () => {
      try {
        const wasControlled = navigator.serviceWorker.controller !== null;
        const registrations = await navigator.serviceWorker.getRegistrations();
        let unregisteredCount = 0;
        await Promise.all(
          registrations.map(async (registration) => {
            const scope = registration.scope;
            const unregistered = await registration.unregister();
            // One warn per evicted scope so the (unexpected) foreign worker is diagnosable.
            if (unregistered) {
              unregisteredCount += 1;
              console.warn(`Unregistered a foreign service worker (FragForge registers none): ${scope}`);
            }
          }),
        );
        let alreadyReloaded = false;
        try {
          alreadyReloaded = sessionStorage.getItem(SW_EVICTED_RELOAD_KEY) !== null;
        } catch {
          // Storage blocked: treat as already reloaded so we never risk a loop.
          alreadyReloaded = true;
        }
        if (shouldReloadAfterEviction({ wasControlled, unregisteredCount, alreadyReloaded })) {
          sessionStorage.setItem(SW_EVICTED_RELOAD_KEY, '1');
          window.location.reload();
        }
      } catch {
        // Never let cleanup break the app; a leftover worker is a nuisance, not fatal.
      }
    })();
  }, []);

  return null;
}
