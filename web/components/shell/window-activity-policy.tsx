'use client';

import { useEffect } from 'react';
import { getDesktopSettingsBridge } from '@/lib/desktop-settings';
import { browserWindowActivity, pausePlayingMedia } from '@/lib/window-activity';

const ACTIVITY_ATTRIBUTE = 'data-window-activity';

/**
 * Exposes native window activity to CSS. Effects remain available while the
 * user is working, then freeze as soon as Studio is minimized, covered, or
 * loses focus so they cannot keep the GPU compositor awake in the background.
 */
export function WindowActivityPolicy() {
  useEffect(() => {
    const root = document.documentElement;
    if (getDesktopSettingsBridge() !== null) {
      root.setAttribute('data-runtime', 'desktop');
      root.setAttribute('data-performance-profile', 'efficiency');
    }
    const update = (): void => {
      const active = browserWindowActivity.isActive();
      root.setAttribute(ACTIVITY_ATTRIBUTE, active ? 'active' : 'inactive');
      if (!active) {
        // Video decode is not covered reliably by timer throttling because
        // browsers intentionally allow background audio/video playback.
        pausePlayingMedia(document.querySelectorAll<HTMLMediaElement>('video, audio'));
      }
    };

    update();
    const unsubscribe = browserWindowActivity.subscribe(update);
    return () => {
      unsubscribe();
      root.removeAttribute(ACTIVITY_ATTRIBUTE);
      root.removeAttribute('data-runtime');
      root.removeAttribute('data-performance-profile');
    };
  }, []);

  return null;
}
