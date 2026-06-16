'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Loader2, UploadCloud } from 'lucide-react';
import { useSession } from '@/lib/session';
import { Wordmark } from '@/components/brand';
import { Button } from '@/components/ui/button';
import { SteamButton } from '@/components/login/steam-button';
import { HeroReel } from '@/components/login/hero-reel';

export default function LoginPage() {
  const router = useRouter();
  const { session, loading, signIn } = useSession();
  const [signingIn, setSigningIn] = useState(false);

  // Once a session exists, send the user where they belong: onboarding if their
  // match history isn't linked yet, otherwise straight into the studio.
  useEffect(() => {
    if (loading || !session?.user) return;
    router.replace(session.matchHistoryLinked ? '/matches' : '/connect');
  }, [loading, session, router]);

  async function handleSignIn() {
    setSigningIn(true);
    try {
      await signIn();
      // Navigation is handled by the effect above once the session updates.
    } catch {
      setSigningIn(false);
    }
  }

  // While we resolve the initial session (or are redirecting a signed-in user),
  // show a quiet loader instead of flashing the landing page.
  if (loading || session?.user) {
    return (
      <main className="grid min-h-screen place-items-center">
        <Loader2 className="size-6 animate-spin text-primary" />
      </main>
    );
  }

  return (
    <main className="relative min-h-screen overflow-hidden bg-background">
      {/* Auth-only scanline + vignette, layered above the global grain. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          backgroundImage:
            'repeating-linear-gradient(0deg, transparent 0 3px, oklch(0 0 0 / 0.18) 3px 4px)',
        }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            'radial-gradient(120% 90% at 50% 0%, transparent 40%, oklch(0 0 0 / 0.55) 100%)',
        }}
      />

      <div className="relative mx-auto flex min-h-screen w-full max-w-6xl flex-col px-6">
        <header className="flex h-16 items-center">
          <Wordmark />
        </header>

        <div className="grid flex-1 items-center gap-12 py-12 lg:grid-cols-[1.1fr_0.9fr]">
          {/* Left: headline + CTA + trust line. */}
          <div className="max-w-xl">
            <span className="inline-flex items-center gap-2 rounded-full border border-border bg-card px-3 py-1 text-xs font-medium uppercase tracking-wider text-muted-foreground">
              <span className="size-1.5 rounded-full bg-primary" />
              The replay studio
            </span>

            <h1 className="mt-6 font-[family-name:var(--font-display)] text-5xl font-bold uppercase leading-[0.92] tracking-tight text-balance sm:text-6xl md:text-7xl">
              Forge your frags
              <br />
              into <span className="text-primary">reels</span>
            </h1>

            <p className="mt-6 max-w-md text-pretty text-base leading-relaxed text-muted-foreground sm:text-lg">
              Scan your CS2 demos, pick the play that pops, and let your own rig
              capture, edit and package the highlight reel.
            </p>

            <div className="mt-9 flex flex-col gap-4">
              <div className="flex flex-wrap items-center gap-3">
                <SteamButton onClick={handleSignIn} loading={signingIn} />
                <Button
                  variant="outline"
                  size="lg"
                  asChild
                  className="h-12 gap-2.5 px-6 text-base"
                >
                  <Link href="/upload">
                    <UploadCloud className="size-5" />
                    Upload a demo
                  </Link>
                </Button>
              </div>
              <p className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-muted-foreground">
                No AI. Your POV. Your rig. · No login to analyze a demo.
              </p>
            </div>
          </div>

          {/* Right: stylized CSS reel motif (no real video). */}
          <div className="hidden justify-center lg:flex">
            <HeroReel />
          </div>
        </div>

        <footer className="flex h-16 items-center text-xs text-muted-foreground/70">
          You bring the PC &amp; GPU. We handle the pipeline.
        </footer>
      </div>
    </main>
  );
}
