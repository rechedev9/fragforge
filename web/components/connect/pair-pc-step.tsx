'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { ArrowRight, Check, Copy, Cpu, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { useSession } from '@/lib/session';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { SectionEyebrow } from '@/components/brand';

export type PairPcStepProps = {
  /**
   * Called when the player chooses to enter the studio. Falls back to a
   * /matches navigation when omitted (e.g. standalone previews).
   */
  onEnter?: () => void;
};

/**
 * Step 2 — pair the player's own PC. FragForge records on the user's rig, with
 * their Steam and GPU, so onboarding offers a pairing code their local agent
 * enters. Pairing is optional, though: the player can enter the studio now and
 * pair before their first reel.
 */
export function PairPcStep({ onEnter }: PairPcStepProps = {}) {
  const router = useRouter();
  const { refresh } = useSession();
  const [pairingCode, setPairingCode] = useState<string | null>(null);
  const [paired, setPaired] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [copied, setCopied] = useState(false);

  const checkStatus = useCallback(async () => {
    const status = await api.getPcStatus();
    setPaired(status.paired);
    return status.paired;
  }, []);

  useEffect(() => {
    void checkStatus();
  }, [checkStatus]);

  async function handleGenerate() {
    setGenerating(true);
    try {
      const result = await api.pairPc();
      setPairingCode(result.pairingCode);
      const isPaired = await checkStatus();
      if (isPaired) await refresh();
    } finally {
      setGenerating(false);
    }
  }

  async function handleCopy() {
    if (!pairingCode) return;
    try {
      await navigator.clipboard.writeText(pairingCode);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard may be unavailable; the code stays visible on screen.
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between gap-3">
        <SectionEyebrow label="Step 02" />
        {paired ? (
          <Badge className="gap-1.5">
            <Check className="size-3" aria-hidden />
            Paired
          </Badge>
        ) : (
          <Badge variant="outline" className="gap-1.5 text-muted-foreground">
            Not paired
          </Badge>
        )}
      </div>

      <h2 className="mt-2 font-[family-name:var(--font-display)] text-2xl font-bold tracking-tight">
        Pair your PC
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
        FragForge records on your own rig. Run the agent on your gaming PC and it
        captures plays with your Steam and your GPU — your POV, your hardware.
      </p>

      <ol className="mt-5 space-y-2.5 text-sm text-muted-foreground">
        <Step n={1}>Install the FragForge agent on your gaming PC.</Step>
        <Step n={2}>Generate a pairing code below.</Step>
        <Step n={3}>Enter the code in the agent to link it to your account.</Step>
      </ol>

      {pairingCode ? (
        <div className="mt-5 flex items-center justify-between gap-3 rounded-xl border border-primary/30 bg-primary/5 px-4 py-3">
          <div>
            <p className="text-[0.7rem] font-medium uppercase tracking-[0.18em] text-primary/80">
              Pairing code
            </p>
            <p className="mt-0.5 font-[family-name:var(--font-mono)] text-2xl font-bold tracking-widest tabular-nums text-foreground">
              {pairingCode}
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={handleCopy}
            aria-label="Copy pairing code"
          >
            {copied ? (
              <Check className="size-4" aria-hidden />
            ) : (
              <Copy className="size-4" aria-hidden />
            )}
            {copied ? 'Copied' : 'Copy'}
          </Button>
        </div>
      ) : null}

      <div className="mt-6 flex flex-col gap-3">
        <Button
          variant={pairingCode ? 'outline' : 'default'}
          size="lg"
          className="w-full"
          onClick={handleGenerate}
          disabled={generating}
        >
          {generating ? (
            <>
              <Loader2 className="size-4 animate-spin" aria-hidden />
              Generating…
            </>
          ) : (
            <>
              <Cpu className="size-4" aria-hidden />
              {pairingCode ? 'Generate a new code' : 'Generate pairing code'}
            </>
          )}
        </Button>

        <Button
          size="lg"
          className="w-full"
          onClick={() => (onEnter ? onEnter() : router.push('/matches'))}
        >
          {pairingCode ? 'Enter the studio' : 'Skip pairing — enter the studio'}
          <ArrowRight className="size-4" aria-hidden />
        </Button>
      </div>
    </div>
  );
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-3">
      <span className="mt-px grid size-5 shrink-0 place-items-center rounded-full bg-secondary font-[family-name:var(--font-mono)] text-[0.7rem] tabular-nums text-secondary-foreground">
        {n}
      </span>
      <span className="leading-relaxed">{children}</span>
    </li>
  );
}
