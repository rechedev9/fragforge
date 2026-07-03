/**
 * GrainOverlay — the global NEON HUD scanlines layer.
 *
 * A fixed, non-interactive overlay rendered once at the app root: 1px white
 * lines every 4px at near-zero opacity, the CRT/replay texture of the skin.
 * All styling lives in `.neon-scanlines` (globals.css); it is static (no
 * animation), so it needs no reduced-motion exception.
 */
export function GrainOverlay() {
  return <div aria-hidden className="neon-scanlines" />;
}
