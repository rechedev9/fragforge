import Link from 'next/link';
import { Wordmark } from '@/components/brand/wordmark';

/** Branded 404 — replaces Next's default unstyled page, with a way back. */
export default function NotFound() {
  return (
    <main className="relative flex min-h-svh flex-col items-center justify-center gap-6 px-6 text-center">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 [background:radial-gradient(800px_460px_at_50%_0%,color-mix(in_oklch,var(--destructive)_8%,transparent),transparent_65%)]"
      />

      <Link href="/" className="absolute left-6 top-6">
        <Wordmark />
      </Link>

      <p className="relative font-[family-name:var(--font-mono)] text-sm uppercase tracking-[0.3em] text-destructive tabular-nums">
        404
      </p>
      <h1 className="relative font-[family-name:var(--font-display)] text-4xl font-bold uppercase tracking-tight sm:text-5xl">
        Esta página ha sido fraggeada
      </h1>
      <p className="relative max-w-md text-sm text-muted-foreground">
        No encontramos esa página. Vuelve y elige una partida para forjarla en un reel.
      </p>

      <div className="relative mt-2 flex flex-wrap items-center justify-center gap-3">
        <Link
          href="/matches"
          className="neon-notch neon-glow inline-flex h-10 items-center gap-2 bg-primary px-6 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
        >
          Volver a partidas
        </Link>
        <Link
          href="/"
          className="inline-flex h-10 items-center gap-2 border border-primary/35 px-6 font-[family-name:var(--font-display)] text-sm font-semibold tracking-[0.06em] text-primary transition-colors hover:bg-primary/10"
        >
          Inicio
        </Link>
      </div>
    </main>
  );
}
