'use client';

/**
 * Tracks whether the player has finished (or skipped) onboarding, so the landing
 * stops bouncing them back to /connect once they've entered the studio. Pure
 * client state — onboarding is optional in this build (the mock has no real
 * agent), so a skip should "stick" without touching the API session contract.
 */
const KEY = 'fragforge.onboarded.v1';

export function dismissOnboarding(): void {
  if (typeof window === 'undefined') return;
  try {
    window.sessionStorage.setItem(KEY, '1');
  } catch {
    // sessionStorage may throw (quota / privacy mode); non-fatal.
  }
}

export function isOnboardingDismissed(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return window.sessionStorage.getItem(KEY) === '1';
  } catch {
    return false;
  }
}
