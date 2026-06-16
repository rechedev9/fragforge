/**
 * GrainOverlay — the global film-grain texture layer.
 *
 * A fixed, non-interactive SVG noise field rendered once at the app root. It
 * gives every screen the subtle tape/replay grain from the design language.
 * Opacity and blend mode live in `.fragforge-grain` (globals.css).
 */
export function GrainOverlay() {
  return (
    <div aria-hidden className="fragforge-grain">
      <svg className="size-full" xmlns="http://www.w3.org/2000/svg">
        <filter id="fragforge-grain-noise">
          <feTurbulence
            type="fractalNoise"
            baseFrequency="0.8"
            numOctaves="3"
            stitchTiles="stitch"
          />
          <feColorMatrix type="saturate" values="0" />
        </filter>
        <rect width="100%" height="100%" filter="url(#fragforge-grain-noise)" />
      </svg>
    </div>
  );
}
