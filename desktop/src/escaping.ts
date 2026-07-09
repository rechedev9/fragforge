// Pure string-escaping helpers used by the Electron main process. Kept in their
// own module (with no side effects and no relative imports) so they can be unit
// tested directly with `node --test`, which runs these .ts files as-is.

/**
 * Escapes the HTML-sensitive characters in untrusted text (log lines, error
 * messages) before it is dropped into a loaded page. Accepts any value and
 * stringifies it first, so a non-string (an Error, undefined) is handled the
 * same way the original inline version did.
 */
export function escapeHtml(s: unknown): string {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** Single-quote a string for a PowerShell command ('' escapes ' inside). */
export function psQuote(s: string): string {
  return `'${s.replace(/'/g, "''")}'`;
}
