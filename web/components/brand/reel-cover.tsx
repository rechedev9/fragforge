import { Crosshair } from 'lucide-react';
import { cn } from '@/lib/utils';

export type ReelCoverProps = {
  /** Stable key (id, map, title) — derives a deterministic dark tint. */
  seed: string;
  /** Optional faint map/label watermark in the corner. */
  label?: string;
  /** Drop the crosshair + label (when the parent overlays its own icon). */
  plain?: boolean;
  className?: string;
};

/**
 * Deterministic hue from a seed string (no Math.random), constrained to the
 * NEON HUD band — cyan (190) through violet to magenta (330) — so covers stay
 * on-skin instead of drifting into lime/orange.
 */
function hueFromSeed(seed: string): number {
  let h = 0;
  for (let i = 0; i < seed.length; i += 1) {
    h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  }
  return 190 + (h % 141);
}

/**
 * ReelCover — a CSS-only, on-brand placeholder for clip thumbnails. Instead of
 * random stock photos, it renders a night-navy cover with a seeded low-chroma
 * tint from the skin's cyan/violet/magenta band, faint scanlines, and a
 * crosshair, so cards read as CS2 reel covers. The seed keeps each cover
 * stable and distinct. Replace with real frames later.
 */
export function ReelCover({ seed, label, plain = false, className }: ReelCoverProps) {
  const hue = hueFromSeed(seed);
  const tint = `hsl(${hue} 55% 18%)`;
  const tint2 = `hsl(${(hue + 28) % 360} 45% 10%)`;

  return (
    <div
      aria-hidden
      className={cn('relative size-full overflow-hidden bg-[#060a14]', className)}
      style={{
        backgroundImage: `radial-gradient(120% 90% at 78% 18%, ${tint} 0%, ${tint2} 42%, #060a14 78%)`,
      }}
    >
      <div
        className="pointer-events-none absolute inset-0 opacity-[0.06] mix-blend-screen"
        style={{
          backgroundImage: 'repeating-linear-gradient(0deg, #fff 0 1px, transparent 1px 3px)',
        }}
      />
      {!plain ? (
        <>
          <div className="pointer-events-none absolute inset-0 grid place-items-center">
            <Crosshair className="size-10 text-white/10" strokeWidth={1.25} />
          </div>
          {label ? (
            <span className="pointer-events-none absolute bottom-2 left-2.5 font-[family-name:var(--font-display)] text-[0.7rem] font-semibold uppercase tracking-[0.18em] text-white/25">
              {label}
            </span>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
