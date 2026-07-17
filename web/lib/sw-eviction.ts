/**
 * Decision logic for the service-worker eviction in ServiceWorkerCleanup,
 * kept pure so the reload rule is unit-testable without a browser.
 */

/** sessionStorage key marking that this tab already reloaded after an eviction. */
export const SW_EVICTED_RELOAD_KEY = 'fragforge:sw-evicted-reload';

/**
 * Whether the page must reload once after evicting service workers.
 *
 * Unregistering a service worker does NOT release a page it already controls:
 * the stale worker keeps intercepting this document's fetches (including
 * <video> media requests) until a full navigation. So when the current page was
 * controlled and we actually unregistered something, one reload is required for
 * the eviction to take effect on this very visit. The alreadyReloaded flag
 * (persisted per-tab in sessionStorage) guards against a reload loop if a
 * worker somehow re-registers or an unregister does not stick.
 */
export function shouldReloadAfterEviction(input: {
  wasControlled: boolean;
  unregisteredCount: number;
  alreadyReloaded: boolean;
}): boolean {
  return input.wasControlled && input.unregisteredCount > 0 && !input.alreadyReloaded;
}
