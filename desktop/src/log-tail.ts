// Pure last-N-lines slicing extracted from logTail() so the tail logic can be
// unit tested without reading studio.log from disk. No side effects, no relative
// imports, so `node --test` runs this .ts file directly.

/** Returns the last `maxLines` lines of `text`, newline-joined, in original order. */
export function lastLines(text: string, maxLines: number): string {
  return text.split('\n').slice(-maxLines).join('\n');
}
