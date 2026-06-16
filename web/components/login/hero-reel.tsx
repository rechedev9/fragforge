import { Crosshair } from 'lucide-react';
import { cn } from '@/lib/utils';

/**
 * A purely decorative CSS film reel: a tilted strip of "frames" with sprocket
 * holes down each edge and one lime-ringed frame to echo the in-app selection
 * ring. No real video — texture only, sitting behind the grain layer.
 */
export function HeroReel({ className }: { className?: string }) {
  // Which frame gets the lime selection ring (the "chosen play").
  const active = 2;

  return (
    <div
      aria-hidden
      className={cn('pointer-events-none select-none', className)}
    >
      <div className="rotate-6">
        <div className="flex flex-col gap-4 rounded-xl border border-border bg-card/60 p-3 shadow-2xl shadow-black/40">
          {[0, 1, 2, 3].map((row) => (
            <FilmRow key={row} active={row === active} index={row} />
          ))}
        </div>
      </div>
    </div>
  );
}

function FilmRow({ active, index }: { active: boolean; index: number }) {
  return (
    <div className="flex items-stretch gap-2">
      <Sprockets />
      <div
        className={cn(
          'relative grid h-28 w-72 place-items-center overflow-hidden rounded-md border bg-secondary',
          active ? 'border-primary ring-2 ring-primary/60' : 'border-border',
        )}
      >
        {/* Faux scene gradient — different lean per frame for variety. */}
        <div
          className="absolute inset-0 opacity-70"
          style={{
            background: `linear-gradient(${120 + index * 35}deg, oklch(0.22 0.01 264), oklch(0.16 0.006 264))`,
          }}
        />
        <Crosshair
          className={cn(
            'relative size-7',
            active ? 'text-primary' : 'text-muted-foreground/60',
          )}
        />
        {active ? (
          <span className="absolute bottom-1.5 left-2 font-[family-name:var(--font-mono)] text-[10px] tabular-nums text-primary">
            R12 · 4K
          </span>
        ) : null}
      </div>
      <Sprockets />
    </div>
  );
}

/** A vertical run of film sprocket holes. */
function Sprockets() {
  return (
    <div className="flex flex-col justify-between py-1">
      {[0, 1, 2, 3].map((i) => (
        <span key={i} className="size-2 rounded-[2px] bg-background/80" />
      ))}
    </div>
  );
}
