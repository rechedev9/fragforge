'use client';

/**
 * Token-aware media access for hosted mode.
 *
 * In hosted mode a reel's downloadUrl / thumbnailUrl (and a stream's source /
 * render URLs) are ABSOLUTE agent URLs. A bare `<video src>` or `<a download>`
 * cannot carry the X-FragForge-Token header, so the browser would hit the agent
 * with no token and be rejected (or CORS-blocked). Instead we fetch the bytes
 * WITH the token into a Blob and hand the UI an object URL / trigger a save. In
 * every other mode the URLs are same-origin relative paths that need no token,
 * so these helpers pass the URL straight through unchanged.
 *
 * The bytes stream browser <- local agent only; they never touch our server.
 */

import { useEffect, useState } from 'react';
import { agentHeaders } from './connection';
import { isHostedMode } from '@/lib/mode';

/** True for an absolute http(s) URL, i.e. a hosted-mode agent URL. */
function isAbsolute(url: string): boolean {
  return /^https?:\/\//i.test(url);
}

/** Whether a URL must be fetched with the agent token (hosted + absolute). */
function needsToken(url: string): boolean {
  return isHostedMode() && isAbsolute(url);
}

/** Fetches an agent artifact with the token header, returning a Blob. */
async function fetchBlob(url: string): Promise<Blob> {
  const res = await fetch(url, { headers: agentHeaders(), cache: 'no-store' });
  if (!res.ok) throw new Error(`media fetch failed (${res.status})`);
  return res.blob();
}

/**
 * Downloads a media artifact to the user's disk. In hosted mode it fetches the
 * bytes with the token into a Blob and saves via a transient object URL; in
 * local/cloud it uses a plain same-origin anchor download. Best-effort: a hosted
 * fetch failure is swallowed by the caller's try/catch surface.
 */
export async function downloadMedia(url: string, filename: string): Promise<void> {
  if (typeof document === 'undefined') return;
  let href = url;
  let objectUrl: string | null = null;
  if (needsToken(url)) {
    const blob = await fetchBlob(url);
    objectUrl = URL.createObjectURL(blob);
    href = objectUrl;
  }
  try {
    const a = document.createElement('a');
    a.href = href;
    a.download = filename;
    a.rel = 'noopener';
    document.body.appendChild(a);
    a.click();
    a.remove();
  } finally {
    // Revoke after the click has been dispatched so the download can start.
    if (objectUrl) setTimeout(() => URL.revokeObjectURL(objectUrl as string), 60_000);
  }
}

/**
 * Resolves a media URL to something a `<video>`/`<img>` `src` can use. In hosted
 * mode it fetches the agent artifact with the token into an object URL (revoked
 * on unmount / url change); otherwise it returns the same-origin URL unchanged.
 * Returns null while a hosted fetch is in flight or has failed, so callers can
 * render a placeholder.
 */
export function useMediaSrc(url: string | undefined): string | null {
  const [resolved, setResolved] = useState<string | null>(url && !needsToken(url) ? url : null);

  useEffect(() => {
    if (!url) {
      setResolved(null);
      return;
    }
    if (!needsToken(url)) {
      setResolved(url);
      return;
    }
    let objectUrl: string | null = null;
    let cancelled = false;
    setResolved(null);
    fetchBlob(url)
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setResolved(objectUrl);
      })
      .catch(() => {
        if (!cancelled) setResolved(null);
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [url]);

  return resolved;
}
