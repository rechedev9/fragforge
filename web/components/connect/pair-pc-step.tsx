'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Check, Copy, Cpu, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { cn } from '@/lib/utils';

// The FragForge Studio Windows installer, the same canonical release asset
// the landing page's download CTA points at. Kept as a literal here since
// web/ and landing/ are separate Next apps with no shared config; bump both
// in lockstep on a new release.
const AGENT_DOWNLOAD_URL =
  'https://github.com/rechedev9/fragforge/releases/download/v0.2.10/FragForge.Studio.Setup.0.2.10.exe';

export type PairPcStepProps = {
  /**
   * Called when the player chooses to enter the studio. Falls back to a
   * /matches navigation when omitted (e.g. standalone previews).
   */
  onEnter?: () => void;
  /** Reports the redeemed-pairing status so the parent stepper can advance. */
  onPairedChange?: (paired: boolean) => void;
};

/**
 * Step 2 — pair the player's own PC. FragForge records on the user's rig, with
 * their Steam and GPU, so onboarding offers a pairing code their local agent
 * enters. Pairing is optional, though: the player can enter the studio now and
 * pair before their first reel.
 */
export function PairPcStep({ onEnter, onPairedChange }: PairPcStepProps = {}) {
  const router = useRouter();
  const [pairingCode, setPairingCode] = useState<string | null>(null);
  const [paired, setPaired] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [copied, setCopied] = useState(false);

  const checkStatus = useCallback(async () => {
    const status = await api.getPcStatus();
    setPaired(status.paired);
    onPairedChange?.(status.paired);
    return status.paired;
  }, [onPairedChange]);

  useEffect(() => {
    void checkStatus();
  }, [checkStatus]);

  async function handleGenerate() {
    setGenerating(true);
    try {
      const result = await api.pairPc();
      setPairingCode(result.pairingCode);
      await checkStatus();
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

  function enter() {
    if (onEnter) onEnter();
    else router.push('/matches');
  }

  if (pairingCode) {
    return (
      <div className="text-center">
        <h1 className="font-[family-name:var(--font-mono)] text-[11px] font-normal uppercase tracking-[0.3em] text-primary">
          Código de emparejamiento
        </h1>

        <div className="mt-6 flex flex-wrap items-center justify-center gap-2.5">
          {pairingCode.split('').map((ch, index) =>
            ch === '-' ? (
              <span
                key={index}
                aria-hidden
                className="flex items-center px-0.5 font-[family-name:var(--font-mono)] text-2xl text-muted-foreground/40"
              >
                –
              </span>
            ) : (
              <span
                key={index}
                className="grid h-[56px] w-[42px] place-items-center border border-primary/40 bg-background/80 font-[family-name:var(--font-mono)] text-2xl text-foreground sm:h-16 sm:w-[50px] sm:text-[28px]"
              >
                {ch}
              </span>
            ),
          )}
        </div>

        <button
          type="button"
          onClick={handleCopy}
          className="mt-3 inline-flex items-center gap-1.5 font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.16em] text-muted-foreground transition-colors hover:text-foreground"
        >
          {copied ? <Check className="size-3" aria-hidden /> : <Copy className="size-3" aria-hidden />}
          {copied ? 'Copiado' : 'Copiar código'}
        </button>

        <p className="mx-auto mt-5 max-w-sm text-sm leading-relaxed text-muted-foreground">
          Abre el <span className="text-foreground">agente FragForge</span> en tu PC gaming y
          escribe este código.
          <br />
          Es lo que graba y renderiza tus reels en tu propio rig.
        </p>

        <div
          className={cn(
            'mt-5 inline-flex items-center gap-2 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.16em]',
            paired ? 'text-primary' : 'text-destructive',
          )}
        >
          <span
            className={cn(
              'size-[7px] rounded-full',
              paired
                ? 'bg-primary shadow-[0_0_8px_var(--primary)]'
                : 'bg-destructive shadow-[0_0_8px_var(--destructive)] neon-pulse',
            )}
          />
          {paired ? 'Agente conectado' : 'Esperando al agente…'}
        </div>

        <div className="mt-6 flex flex-wrap items-center justify-center gap-3.5">
          <a
            href={AGENT_DOWNLOAD_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="neon-notch inline-flex items-center bg-primary px-6 py-2.5 font-[family-name:var(--font-display)] text-[13px] font-bold tracking-[0.05em] text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Descargar agente (Windows)
          </a>
          <button type="button" onClick={enter} className="border border-primary/35 px-6 py-2.5 font-[family-name:var(--font-display)] text-[13px] font-semibold tracking-[0.05em] text-primary transition-colors hover:bg-primary/10">
            Ya lo tengo — entrar al estudio
          </button>
        </div>

        <button
          type="button"
          onClick={handleGenerate}
          disabled={generating}
          className="mt-4 inline-flex items-center gap-1.5 font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.14em] text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
        >
          {generating ? <Loader2 className="size-3 animate-spin" aria-hidden /> : null}
          Generar otro código
        </button>
      </div>
    );
  }

  return (
    <div className="text-left">
      <div className="flex items-center justify-between gap-3">
        <SectionEyebrow number={2} label="EMPAREJA TU PC" />
        {paired ? (
          <Badge className="gap-1.5">
            <Check className="size-3" aria-hidden />
            Emparejado
          </Badge>
        ) : (
          <Badge variant="outline" className="gap-1.5 text-muted-foreground">
            No emparejado
          </Badge>
        )}
      </div>

      <h1 className="mt-2 font-[family-name:var(--font-display)] text-2xl font-bold uppercase tracking-tight text-foreground">
        Empareja tu PC
      </h1>
      <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
        FragForge graba en tu propio equipo. Ejecuta el agente en tu PC gaming y
        captura tus jugadas con tu Steam y tu GPU — tu POV, tu hardware.
      </p>

      <ol className="mt-5 space-y-2.5 text-sm text-muted-foreground">
        <Step n={1}>Instala el agente FragForge en tu PC gaming.</Step>
        <Step n={2}>Genera un código de emparejamiento abajo.</Step>
        <Step n={3}>Escribe el código en el agente para vincularlo a tu cuenta.</Step>
      </ol>

      <div className="mt-5 border border-border bg-card/50 p-4">
        <p className="text-sm font-medium text-foreground">La captura necesita HLAE + CS2 en este PC</p>
        <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
          La grabación controla CS2 a través de HLAE. Apunta el orquestador a ellos con
          estas variables de entorno y reinícialo:
        </p>
        <ul className="mt-2 space-y-1 font-[family-name:var(--font-mono)] text-xs text-muted-foreground">
          <li>ZV_RECORDER_PATH</li>
          <li>ZV_HLAE_PATH</li>
          <li>ZV_CS2_PATH</li>
        </ul>
        <p className="mt-2 text-xs text-muted-foreground/80">
          La tarjeta CAPTURA del panel lateral muestra si están configuradas y accesibles.
        </p>
      </div>

      <div className="mt-6 flex flex-col gap-3">
        <Button
          size="lg"
          className="neon-notch neon-glow w-full font-[family-name:var(--font-display)] font-bold tracking-[0.06em]"
          onClick={handleGenerate}
          disabled={generating}
        >
          {generating ? (
            <>
              <Loader2 className="size-4 animate-spin" aria-hidden />
              Generando…
            </>
          ) : (
            <>
              <Cpu className="size-4" aria-hidden />
              Generar código de emparejamiento
            </>
          )}
        </Button>

        <Button
          variant="outline"
          size="lg"
          className="w-full font-[family-name:var(--font-display)] font-semibold tracking-[0.06em]"
          onClick={enter}
        >
          Saltar emparejamiento — entrar al estudio
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
