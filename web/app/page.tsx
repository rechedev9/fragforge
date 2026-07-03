'use client';

import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Loader2 } from 'lucide-react';
import { useSession } from '@/lib/session';
import { isLocalMode } from '@/lib/mode';
import { isOnboardingDismissed } from '@/lib/onboarding';
import { Wordmark } from '@/components/brand';
import { SteamButton } from '@/components/login/steam-button';

// Client-only: three.js touches WebGL, so it never renders on the server.
const HeroThree = dynamic(() => import('@/components/login/hero-three'), {
  ssr: false,
});

/** The four HUD corner brackets of a highlighted route card. */
function CardCorners({ color }: { color: 'primary' | 'destructive' }) {
  const b = color === 'primary' ? 'border-primary' : 'border-destructive';
  return (
    <>
      <span aria-hidden className={`absolute -top-px -left-px size-4 border-t-2 border-l-2 ${b}`} />
      <span aria-hidden className={`absolute -top-px -right-px size-4 border-t-2 border-r-2 ${b}`} />
      <span aria-hidden className={`absolute -bottom-px -left-px size-4 border-b-2 border-l-2 ${b}`} />
      <span aria-hidden className={`absolute -bottom-px -right-px size-4 border-b-2 border-r-2 ${b}`} />
    </>
  );
}

/** Static pipeline strip: first stage lit cyan, the rest dim, all mono. */
const PIPELINE = ['ANÁLISIS', 'CAPTURA', 'EDICIÓN', 'REEL'] as const;

export default function LoginPage() {
  const router = useRouter();
  const { session, loading, signIn } = useSession();
  const [signingIn, setSigningIn] = useState(false);

  // Local studio has no Steam login: the dashboard is home, so any navigation
  // to the cloud landing (stale links, browser Back) bounces straight there.
  useEffect(() => {
    if (isLocalMode()) router.replace('/matches');
  }, [router]);

  // Once a session exists, send the user where they belong: onboarding if their
  // match history isn't linked yet, otherwise straight into the studio.
  useEffect(() => {
    if (loading || !session?.user) return;
    const ready = session.matchHistoryLinked || isOnboardingDismissed();
    router.replace(ready ? '/matches' : '/connect');
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

  // While we resolve the initial session (or are redirecting a signed-in or
  // local-studio user), show a quiet loader instead of flashing the landing.
  if (isLocalMode() || loading || session?.user) {
    return (
      <main className="grid min-h-screen place-items-center">
        <Loader2 className="size-6 animate-spin text-primary" />
      </main>
    );
  }

  return (
    <main className="relative flex min-h-screen flex-col overflow-hidden bg-background">
      {/* 3D film reel, recolored to the HUD cyan, full-bleed behind the hero. */}
      <HeroThree className="absolute inset-0 z-0 opacity-70" />

      {/* Ambient radial glows: cyan up top, magenta bottom-right (mockup 1a). */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 z-[1]"
        style={{
          background:
            'radial-gradient(900px 500px at 50% -10%, color-mix(in oklch, var(--primary) 10%, transparent), transparent 65%), radial-gradient(700px 400px at 85% 105%, color-mix(in oklch, var(--destructive) 9%, transparent), transparent 60%)',
        }}
      />

      {/* Top bar: wordmark left; capture status line + Steam session entry right.
          The status line is static copy (mockup 1a): the cloud landing has no
          capture telemetry pre-login — the live probe is the in-app sidebar card. */}
      <header className="relative z-10 flex items-center justify-between gap-4 border-b border-primary/15 px-6 py-4 sm:px-11">
        <Wordmark />
        <div className="flex items-center gap-5">
          <span className="hidden items-center gap-2.5 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.08em] text-muted-foreground lg:flex">
            <span
              aria-hidden
              className="size-[7px] rounded-full bg-primary [box-shadow:0_0_8px_var(--primary)]"
            />
            CAPTURA LISTA · HLAE + CS2 DETECTADO
          </span>
          <SteamButton onClick={handleSignIn} loading={signingIn} />
        </div>
      </header>

      {/* Hero */}
      <div className="relative z-10 flex flex-1 flex-col items-center px-6 pt-12 text-center sm:pt-14">
        <p className="font-[family-name:var(--font-mono)] text-[13px] uppercase tracking-[0.32em] text-primary">
          {'// SISTEMA DE REPLAY — EN LÍNEA'}
        </p>
        <h1 className="mt-3.5 font-[family-name:var(--font-display)] text-4xl font-bold leading-none tracking-[0.01em] text-foreground [text-shadow:0_0_28px_color-mix(in_oklch,var(--primary)_35%,transparent)] sm:text-5xl md:text-[62px]">
          FORJA TU HIGHLIGHT
        </h1>
        <p className="mt-4 max-w-xl text-base leading-relaxed text-muted-foreground">
          Tus mejores jugadas, encontradas y montadas en tu propio PC. Elige tu
          fuente.
        </p>

        {/* Route cards: 01 / DEMO (cyan) and 02 / STREAM (magenta). */}
        <div className="mt-11 flex w-full max-w-[976px] flex-col justify-center gap-7 pb-12 text-left lg:flex-row">
          <div className="relative flex-1 border border-primary/35 bg-card/85 p-7 pb-6">
            <CardCorners color="primary" />
            <div className="flex items-center justify-between font-[family-name:var(--font-mono)]">
              <span className="text-xs tracking-[0.24em] text-primary">01 / DEMO</span>
              <span className="text-[10px] tracking-[0.14em] text-muted-foreground/70">.DEM</span>
            </div>
            <h2 className="mt-4 font-[family-name:var(--font-display)] text-[28px] font-bold leading-none text-foreground">
              ANALIZAR DEMO
            </h2>
            <p className="mt-2.5 text-sm leading-relaxed text-muted-foreground">
              Suelta un .dem de CS2 — tuyo o de cualquiera. Detectamos las
              mejores rondas y las forjamos en un reel.
            </p>
            <div className="mt-5">
              <Link
                href="/upload"
                className="neon-notch neon-glow inline-flex h-11 items-center bg-primary px-6 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
              >
                SOLTAR UN .DEM
              </Link>
            </div>
            <p className="mt-4 font-[family-name:var(--font-mono)] text-[10.5px] tracking-[0.16em] text-muted-foreground/70">
              SIN LOGIN · EL .DEM NUNCA SALE DE TU PC
            </p>
          </div>

          <div className="relative flex-1 border border-destructive/40 bg-destructive/10 p-7 pb-6">
            <CardCorners color="destructive" />
            <div className="flex items-center justify-between font-[family-name:var(--font-mono)]">
              <span className="text-xs tracking-[0.24em] text-destructive">02 / STREAM</span>
              <span className="text-[10px] tracking-[0.14em] text-muted-foreground/70">9:16 · 16:9</span>
            </div>
            <h2 className="mt-4 font-[family-name:var(--font-display)] text-[28px] font-bold leading-none text-foreground">
              CLIPS DE STREAM
            </h2>
            <p className="mt-2.5 text-sm leading-relaxed text-muted-foreground">
              Pega un clip de Twitch o YouTube — o sube un MP4 — y córtalo en
              Shorts con tu facecam encima.
            </p>
            <div className="mt-5">
              <Link
                href="/streams"
                className="neon-notch inline-flex h-11 items-center border-[1.5px] border-destructive px-6 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-destructive transition-colors hover:bg-destructive/15"
              >
                PEGAR ENLACE
              </Link>
            </div>
            <p className="mt-4 font-[family-name:var(--font-mono)] text-[10.5px] tracking-[0.16em] text-muted-foreground/70">
              TWITCH · YOUTUBE · MP4
            </p>
          </div>
        </div>
      </div>

      {/* Pipeline footer: static strip, first stage lit. */}
      <footer className="relative z-10 flex flex-wrap items-center justify-center gap-x-5 gap-y-1 border-t border-primary/15 px-6 py-5 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.2em]">
        {PIPELINE.map((stage, i) => (
          <span key={stage} className="flex items-center gap-5">
            {i > 0 ? (
              <span aria-hidden className="text-muted-foreground/40">
                ▸
              </span>
            ) : null}
            <span
              className={
                i === 0
                  ? 'text-primary [text-shadow:0_0_10px_color-mix(in_oklch,var(--primary)_60%,transparent)]'
                  : 'text-muted-foreground'
              }
            >
              {stage}
            </span>
          </span>
        ))}
        <span aria-hidden className="ml-5 text-muted-foreground/40">
          |
        </span>
        <span className="text-muted-foreground">RENDER: TU RIG</span>
      </footer>
    </main>
  );
}
