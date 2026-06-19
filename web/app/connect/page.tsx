'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { ArrowRight, Loader2 } from 'lucide-react';
import { useSession } from '@/lib/session';
import { dismissOnboarding } from '@/lib/onboarding';
import { Wordmark } from '@/components/brand';
import { Card } from '@/components/ui/card';
import { StepperRail, type StepperStep } from '@/components/connect/stepper-rail';
import { LinkHistoryStep } from '@/components/connect/link-history-step';
import { PairPcStep } from '@/components/connect/pair-pc-step';

const STEPS: StepperStep[] = [
  {
    title: 'Link match history',
    hint: 'Connect Steam so we can scan your demos.',
  },
  {
    title: 'Pair your PC',
    hint: 'Your rig records the highlights locally.',
  },
];

/**
 * Onboarding (/connect). A two-step vertical stepper that gets the player from a
 * fresh sign-in to the studio: link their match history, then pair their PC.
 * Both steps are optional — a persistent "enter the studio" escape hatch means
 * you can jump straight in and finish setup later. The active step resumes from
 * the session, so returning here never re-asks a step you've already done.
 */
export default function ConnectPage() {
  const router = useRouter();
  const { session, loading } = useSession();
  const [step, setStep] = useState(0);

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
      <main className="grid min-h-screen place-items-center">
        <Loader2 className="size-6 animate-spin text-primary" />
      </main>
    );
  }

  return (
    <main className="relative min-h-screen overflow-hidden">
      {/* Faint lime glow behind the onboarding card. */}
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 left-1/2 h-[36rem] w-[36rem] -translate-x-1/2 rounded-full bg-primary/10 blur-[160px]"
      />

      <div className="relative mx-auto flex min-h-screen max-w-3xl flex-col px-6">
        <header className="flex h-16 items-center">
          <Link href="/" aria-label="FragForge home">
            <Wordmark />
          </Link>
        </header>

        <div className="flex flex-1 flex-col justify-center py-12">
          <div className="mb-8 max-w-xl">
            <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold uppercase tracking-tight sm:text-4xl">
              Set up your studio
            </h1>
            <p className="mt-3 text-muted-foreground">
              Two quick steps and you&apos;re ready to forge your frags into
              reels — or jump straight in.
            </p>
          </div>

          <Card className="overflow-hidden p-6 sm:p-8">
            <div className="grid gap-8 sm:grid-cols-[minmax(0,12rem)_1fr] sm:gap-10">
              <StepperRail steps={STEPS} current={step} className="sm:pt-1" />

              <div>
                {step === 0 ? (
                  <LinkHistoryStep onLinked={() => setStep(1)} />
                ) : (
                  <PairPcStep onEnter={enterStudio} />
                )}
              </div>
            </div>
          </Card>

          {/* Always-available escape hatch — never get trapped in onboarding. */}
          <div className="mt-5 flex items-center justify-center">
            <button
              type="button"
              onClick={enterStudio}
              className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              Skip for now — enter the studio
              <ArrowRight className="size-3.5" aria-hidden />
            </button>
          </div>
        </div>

        <footer className="flex h-16 items-center text-xs text-muted-foreground/70">
          You bring the PC &amp; GPU. We handle the rest.
        </footer>
      </div>
    </main>
  );
}
