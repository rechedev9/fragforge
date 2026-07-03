'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Loader2 } from 'lucide-react';
import { useSession } from '@/lib/session';
import { dismissOnboarding } from '@/lib/onboarding';
import { Wordmark } from '@/components/brand/wordmark';
import { StepperRail, type StepperStep } from '@/components/connect/stepper-rail';
import { LinkHistoryStep } from '@/components/connect/link-history-step';
import { PairPcStep } from '@/components/connect/pair-pc-step';

const STEPS: StepperStep[] = [
  { label: 'VINCULA STEAM' },
  { label: 'EMPAREJA TU PC' },
  { label: 'CAPTURA LISTA' },
];

/**
 * Onboarding (/connect), NEON HUD style (mockup 3c): a top bar, a horizontal
 * stepper across the real two-step flow (link Steam match history, then pair
 * a PC) plus a "capture ready" third marker once pairing lands, and a single
 * bracket-cornered card holding whichever step is active. Both steps are
 * optional — a persistent "enter the studio" escape hatch means you can jump
 * straight in and finish setup later. The active step resumes from the
 * session, so returning here never re-asks a step you've already done.
 */
export default function ConnectPage() {
  const router = useRouter();
  const { session, loading } = useSession();
  const [step, setStep] = useState(0);
  const [paired, setPaired] = useState(false);

  // Resume where the player actually is — skip steps already completed.
  useEffect(() => {
    if (session?.matchHistoryLinked) setStep(1);
  }, [session?.matchHistoryLinked]);

  function enterStudio() {
    dismissOnboarding();
    router.push('/matches');
  }

  if (loading) {
    return (
      <main className="grid min-h-screen place-items-center bg-background">
        <Loader2 className="size-6 animate-spin text-primary" />
      </main>
    );
  }

  // The stepper has one more marker than the real flow: "CAPTURA LISTA" lights
  // up once pairing is confirmed, even though it isn't its own page/step.
  let stepperIndex: number;
  if (step === 0) {
    stepperIndex = 0;
  } else if (paired) {
    stepperIndex = 2;
  } else {
    stepperIndex = 1;
  }

  return (
    <main className="relative min-h-screen overflow-hidden">
      {/* Radial cyan glow behind the stepper and card, per mockup 3c. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 [background:radial-gradient(800px_460px_at_50%_0%,color-mix(in_oklch,var(--primary)_8%,transparent),transparent_65%)]"
      />

      <div className="relative mx-auto flex min-h-screen max-w-5xl flex-col px-6 sm:px-11">
        <header className="flex h-20 items-center justify-between">
          <Link href="/" aria-label="Inicio de FragForge">
            <Wordmark />
          </Link>
          <Link
            href="/"
            className="font-[family-name:var(--font-mono)] text-[11px] tracking-[0.22em] text-muted-foreground/70 transition-colors hover:text-primary"
          >
            ◂ VOLVER
          </Link>
        </header>

        <StepperRail steps={STEPS} current={stepperIndex} className="mt-2" />

        <div className="flex flex-1 flex-col items-center justify-center gap-6 py-10">
          <div className="neon-brackets relative w-full max-w-[620px] border border-primary/35 bg-card/85 px-7 py-9 sm:px-11">
            {step === 0 ? (
              <LinkHistoryStep onLinked={() => setStep(1)} />
            ) : (
              <PairPcStep onEnter={enterStudio} onPairedChange={setPaired} />
            )}
          </div>

          {/* Always-available escape hatch — never get trapped in onboarding. */}
          <button
            type="button"
            onClick={enterStudio}
            className="font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.16em] text-muted-foreground transition-colors hover:text-foreground"
          >
            Saltar por ahora — entrar al estudio
          </button>
        </div>

        <footer className="flex h-16 items-center justify-center font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.2em] text-muted-foreground/60">
          TÚ PONES EL PC Y LA GPU · NOSOTROS EL RESTO
        </footer>
      </div>
    </main>
  );
}
