export interface WindowActivity {
  isActive(): boolean;
  subscribe(listener: () => void): () => void;
}

export type PausableMedia = Pick<HTMLMediaElement, 'pause' | 'paused'>;

/** Pause only media that is actually decoding or producing audio. */
export function pausePlayingMedia(mediaElements: Iterable<PausableMedia>): number {
  let pausedCount = 0;
  for (const media of mediaElements) {
    if (media.paused) continue;
    media.pause();
    pausedCount += 1;
  }
  return pausedCount;
}

let focused: boolean | undefined;

export function focusAfterVisibilityChange(
  visibilityState: DocumentVisibilityState,
  hasFocus: boolean,
): boolean {
  return visibilityState === 'visible' && hasFocus;
}

/**
 * Treat an unfocused or hidden Studio window as inactive. Chromium already
 * throttles some background work, but an explicit signal lets application
 * timers and CSS effects stop immediately instead of depending on browser
 * heuristics.
 */
export const browserWindowActivity: WindowActivity = {
  isActive(): boolean {
    if (typeof document === 'undefined') return true;
    return document.visibilityState === 'visible' && (focused ?? document.hasFocus());
  },
  subscribe(listener: () => void): () => void {
    if (typeof document === 'undefined' || typeof window === 'undefined') return () => {};
    focused ??= document.hasFocus();
    const onFocus = (): void => {
      focused = true;
      listener();
    };
    const onBlur = (): void => {
      focused = false;
      listener();
    };
    const onVisibilityChange = (): void => {
      focused = focusAfterVisibilityChange(document.visibilityState, document.hasFocus());
      listener();
    };
    const onPageShow = (): void => {
      focused = document.hasFocus();
      listener();
    };

    document.addEventListener('visibilitychange', onVisibilityChange);
    window.addEventListener('focus', onFocus);
    window.addEventListener('blur', onBlur);
    window.addEventListener('pageshow', onPageShow);
    window.addEventListener('pagehide', onBlur);

    return () => {
      document.removeEventListener('visibilitychange', onVisibilityChange);
      window.removeEventListener('focus', onFocus);
      window.removeEventListener('blur', onBlur);
      window.removeEventListener('pageshow', onPageShow);
      window.removeEventListener('pagehide', onBlur);
    };
  },
};
