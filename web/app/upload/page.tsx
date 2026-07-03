'use client';

import { useCallback, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { ArrowLeft, FileVideo, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { isLocalMode } from '@/lib/mode';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';
import type { DemoPlayer, RosterMatch } from '@/lib/api/types';
import { Wordmark } from '@/components/brand/wordmark';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DemoDropzone } from '@/components/upload/demo-dropzone';
import { PlayerPicker } from '@/components/upload/player-picker';

type Stage = 'idle' | 'scanning' | 'picking' | 'parsing' | 'waiting-for-pc';

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

/** The three pipeline steps under the dropzone (mockup 2b): static copy. */
const PIPELINE_STEPS = [
  {
    n: '01',
    accent: 'text-primary',
    title: 'ANÁLISIS AUTOMÁTICO',
    copy: 'Parseamos la demo y puntuamos cada ronda: clutches, aces, multi-kills.',
  },
  {
    n: '02',
    accent: 'text-primary',
    title: 'ELIGES LAS JUGADAS',
    copy: 'Filmstrip con las mejores jugadas detectadas. Marca las que quieres en el reel.',
  },
  {
    n: '03',
    accent: 'text-destructive',
    title: 'RENDER EN TU RIG',
    copy: 'Captura y edición en tu propio PC. 9:16 para Shorts o 16:9 para largo.',
  },
] as const;

/**
 * Upload flow (/upload) — the no-login entry. Drop any .dem (yours or someone
 * else's), we scan its roster, let you pick whose POV to clip, then parse that
 * player into a match and route into the same highlight → render pipeline as
 * Steam matches. Renders on the root layout (no sidebar): the user isn't
 * necessarily signed in here.
 */
export default function UploadPage() {
  const router = useRouter();
  // "Home" depends on the data plane: the local studio lands on the dashboard
  // (there is no login screen), the cloud on the marketing/login landing.
  const homeHref = isLocalMode() ? '/matches' : '/';
  const [stage, setStage] = useState<Stage>('idle');
  const [fileName, setFileName] = useState<string | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const [players, setPlayers] = useState<DemoPlayer[]>([]);
  const [match, setMatch] = useState<RosterMatch | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);

  const reset = useCallback((message: string) => {
    setError(message);
    setStage('idle');
    setFileName(null);
    setJobId(null);
    setPlayers([]);
    setMatch(null);
  }, []);

  const runScan = useCallback(
    async (file: File) => {
      setError(null);
      setFileName(file.name);
      setPendingFile(file);
      setStage('scanning');
      try {
        const scan = await api.scanDemo(file);
        setJobId(scan.jobId);
        setPlayers(scan.players);
        setMatch(scan.match ?? null);
        setStage('picking');
      } catch (err) {
        if (err instanceof Error && err.message === 'PC_OFFLINE') {
          // Keep fileName/pendingFile so Retry can re-run the same file once
          // the user's PC comes back online; do not clear them via reset.
          setStage('waiting-for-pc');
          return;
        }
        reset(
          isServiceUnavailable(err)
            ? 'El servicio de análisis está offline. Arráncalo y vuelve a intentarlo.'
            : 'No se pudo escanear esa demo. Prueba con otro archivo .dem.',
        );
      }
    },
    [reset],
  );

  const onFile = useCallback(
    (file: File) => {
      if (stage !== 'idle') return;
      void runScan(file);
    },
    [stage, runScan],
  );

  const onPick = useCallback(
    async (steamId: string) => {
      if (stage !== 'picking' || !jobId) return;
      setError(null);
      setStage('parsing');
      try {
        const match = await api.parseDemo({ jobId, steamId });
        router.push('/matches/' + match.id);
      } catch (err) {
        reset(
          isServiceUnavailable(err)
            ? 'El servicio de análisis está offline. Arráncalo y vuelve a intentarlo.'
            : 'No se pudieron extraer los highlights de ese jugador. Elige otro.',
        );
      }
    },
    [stage, jobId, router, reset],
  );

  let cardContent: ReactNode;
  if (stage === 'scanning' || stage === 'parsing') {
    cardContent = (
      <div className="flex flex-col items-center justify-center gap-4 py-14 text-center">
        <Loader2 className="size-8 animate-spin text-primary" />
        <div className="flex flex-col gap-1">
          <p className="font-[family-name:var(--font-display)] text-lg font-bold uppercase tracking-tight text-foreground">
            {stage === 'scanning' ? 'Escaneando el roster…' : 'Forjando highlights…'}
          </p>
          {fileName ? (
            <p className="inline-flex items-center justify-center gap-1.5 font-[family-name:var(--font-mono)] text-sm text-muted-foreground">
              <FileVideo className="size-4" />
              {fileName}
            </p>
          ) : null}
        </div>
      </div>
    );
  } else if (stage === 'picking') {
    cardContent = <PlayerPicker players={players} onPick={onPick} match={match ?? undefined} />;
  } else {
    cardContent = (
      <div className="flex flex-col items-center justify-center gap-4 py-14 text-center">
        <div className="flex flex-col gap-1">
          <p className="font-[family-name:var(--font-display)] text-lg font-bold uppercase tracking-tight text-foreground">
            Tu PC está offline
          </p>
          <p className="text-sm text-muted-foreground">
            Abre FragForge Agent en tu PC para analizar esta demo y reintenta.
          </p>
          {fileName ? (
            <p className="inline-flex items-center justify-center gap-1.5 font-[family-name:var(--font-mono)] text-sm text-muted-foreground">
              <FileVideo className="size-4" />
              {fileName}
            </p>
          ) : null}
        </div>
        <Button
          className="neon-notch font-[family-name:var(--font-display)] font-bold tracking-[0.06em]"
          onClick={() => {
            if (pendingFile) void runScan(pendingFile);
          }}
        >
          REINTENTAR
        </Button>
      </div>
    );
  }

  return (
    <main className="relative min-h-screen overflow-hidden">
      {/* Faint cyan glow, matching the onboarding screen. */}
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 left-1/2 h-[36rem] w-[36rem] -translate-x-1/2 rounded-full bg-primary/10 blur-[160px]"
      />

      <div className="relative mx-auto flex min-h-screen max-w-3xl flex-col px-6">
        <header className="flex h-16 items-center justify-between">
          <Link href={homeHref} aria-label="Inicio de FragForge">
            <Wordmark />
          </Link>
          <Button variant="ghost" size="sm" asChild className="text-muted-foreground">
            <Link href={homeHref}>
              <ArrowLeft className="size-4" />
              Volver
            </Link>
          </Button>
        </header>

        <div className="flex flex-1 flex-col justify-center py-12">
          <div className="mb-8 max-w-xl">
            <SectionEyebrow number={2} label="SUBIR DEMO" className="mb-2.5" />
            <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold leading-none tracking-tight sm:text-[34px]">
              {stage === 'picking' ? '¿A QUIÉN QUIERES CLIPEAR?' : 'ANALIZA CUALQUIER DEMO'}
            </h1>
            <p className="mt-3 text-sm text-muted-foreground">
              {stage === 'picking' ? (
                <>Elige a un jugador de la demo y forjaremos sus mejores jugadas en un reel.</>
              ) : (
                <>
                  Suelta un .dem — tuyo o de cualquiera — y forja sus mejores
                  jugadas en un reel. Sin login.
                </>
              )}
            </p>
          </div>

          {stage === 'idle' ? (
            <div className="flex flex-col gap-3">
              <DemoDropzone onFile={onFile} />
              {error ? <p className="text-sm text-destructive">{error}</p> : null}
              <div className="mt-4 grid gap-4 sm:grid-cols-3">
                {PIPELINE_STEPS.map((step) => (
                  <div key={step.n} className="border border-primary/15 bg-card/75 p-5">
                    <div className={`font-[family-name:var(--font-mono)] text-xl ${step.accent}`}>
                      {step.n}
                    </div>
                    <div className="mt-2 font-[family-name:var(--font-display)] text-[15px] font-bold text-foreground">
                      {step.title}
                    </div>
                    <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                      {step.copy}
                    </p>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <Card className="overflow-hidden p-6 sm:p-8">{cardContent}</Card>
          )}
        </div>

        <footer className="flex h-16 items-center font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.2em] text-muted-foreground/70">
          TÚ PONES EL PC Y LA GPU · NOSOTROS EL RESTO
        </footer>
      </div>
    </main>
  );
}
