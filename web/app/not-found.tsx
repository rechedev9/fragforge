import Link from 'next/link';
import { Wordmark } from '@/components/brand/wordmark';
import { Button } from '@/components/ui/button';

/** Branded 404 — replaces Next's default unstyled page, with a way back. */
export default function NotFound() {
  return (
    <main className="relative flex min-h-svh flex-col items-center justify-center gap-6 px-6 text-center">
      <Link href="/" className="absolute left-6 top-6">
        <Wordmark />
      </Link>

      <p className="font-[family-name:var(--font-mono)] text-sm uppercase tracking-[0.3em] text-primary tabular-nums">
        404
      </p>
      <h1 className="font-[family-name:var(--font-display)] text-4xl font-bold tracking-tight sm:text-5xl">
        This page got fragged.
      </h1>
      <p className="max-w-md text-sm text-muted-foreground">
        We couldn&apos;t find that page. Head back and pick a match to forge into a reel.
      </p>

      <div className="mt-2 flex flex-wrap items-center justify-center gap-3">
        <Button asChild size="lg">
          <Link href="/matches">Back to matches</Link>
        </Button>
        <Button asChild variant="outline" size="lg">
          <Link href="/">Home</Link>
        </Button>
      </div>
    </main>
  );
}
