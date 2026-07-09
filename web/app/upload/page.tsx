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
import { StudioPageHeader } from '@/components/studio/page-header';
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
    badge: 'border-primary/35 bg-primary/10',
    title: 'ANÁLISIS AUTOMÁTICO',
    copy: 'Parseamos la demo y puntuamos cada ronda: clutches, aces, multi-kills.',
  },
  {
    n: '02',
    accent: 'text-primary',
    badge: 'border-primary/35 bg-primary/10',
    title: 'ELIGES LAS JUGADAS',
    copy: 'Filmstrip con las mejores jugadas detectadas. Marca las que quieres en el reel.',
  },
  {
    n: '03',
    accent: 'text-primary',
    badge: 'border-primary/35 bg-primary/10',
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
        if (scan.players.length === 0) {
          // A demo can scan "successfully" yet yield an empty roster (e.g. a
          // Source-1 demo: CS:GO/TF2 carry the HL2DEMO magic and pass the header
          // checks, then parse to zero players). Without this guard the flow
          // advances to the picker over an empty card and strands the user.
          reset(
            'El escaneo no encontró jugadores en esa demo. ¿Seguro que es una demo de CS2? Prueba con otro archivo .dem.',
          );
          return;
        }
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
        if (err instanceof Error && err.message === 'PC_OFFLINE') {
          // The PC dropped mid-parse (cloud loopback died). Route to the same
          // waiting-for-pc state as runScan; fileName/pendingFile are still set
          // from the scan, so Retry re-runs the whole file once the PC is back.
          setStage('waiting-for-pc');
          return;
        }
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
      <div role="status" aria-live="polite" className="flex min-h-[260px] flex-col items-center justify-center gap-5 px-4 py-12 text-center">
        <span className="grid size-14 place-items-center border border-primary/35 bg-primary/10 text-primary shadow-[0_0_24px_color-mix(in_oklch,var(--primary)_14%,transparent)]">
          <Loader2 className="size-6 animate-spin" />
        </span>
        <div className="flex flex-col gap-2">
          <p className="font-[family-name:var(--font-display)] text-xl font-bold uppercase tracking-tight text-foreground">
            {stage === 'scanning' ? 'Escaneando el roster…' : 'Forjando highlights…'}
          </p>
          {fileName ? (
            <p className="inline-flex items-center justify-center gap-2 font-[family-name:var(--font-mono)] text-sm text-muted-foreground">
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
      <div role="alert" className="flex min-h-[260px] flex-col items-center justify-center gap-6 px-4 py-12 text-center">
        <div className="flex max-w-xl flex-col gap-2">
          <p className="font-[family-name:var(--font-display)] text-xl font-bold uppercase tracking-tight text-foreground">
            Tu PC está offline
          </p>
          <p className="text-[15px] leading-6 text-muted-foreground">
            Abre FragForge Agent en tu PC para analizar esta demo y reintenta.
          </p>
          <p className="text-[15px] leading-6 text-muted-foreground">
            ¿Primera vez?{' '}
            <Link href="/connect?step=pair" className="font-medium text-primary hover:underline">
              Empareja este PC
            </Link>{' '}
            — sin login.
          </p>
          {fileName ? (
            <p className="mt-2 inline-flex items-center justify-center gap-2 font-[family-name:var(--font-mono)] text-sm text-muted-foreground">
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
    <main className="relative min-h-screen overflow-x-hidden">
      {/* Ambient light keeps the standalone upload entry visually connected to
          the Studio shell without competing with the working surface. */}
      <div
        aria-hidden
        className="pointer-events-none absolute -top-52 left-1/2 h-[40rem] w-[48rem] -translate-x-1/2 rounded-full bg-primary/[0.09] blur-[180px]"
      />
      <div aria-hidden className="pointer-events-none absolute top-[38rem] -right-40 size-[28rem] rounded-full bg-stream/[0.035] blur-[150px]" />

      <div className="relative mx-auto flex min-h-screen w-full max-w-[960px] flex-col px-4 sm:px-6 lg:px-8">
        <header className="flex min-h-16 items-center justify-between border-b border-border/60 py-3">
          <Link href={homeHref} aria-label="Inicio de FragForge">
            <Wordmark />
          </Link>
          <Button variant="ghost" size="sm" asChild className="text-muted-foreground hover:text-foreground">
            <Link href={homeHref}>
              <ArrowLeft className="size-4" />
              Volver
            </Link>
          </Button>
        </header>

        <div className="flex flex-1 flex-col py-8 sm:py-10">
          <StudioPageHeader
            number={2}
            label="SUBIR DEMO"
            title={stage === 'picking' ? '¿A QUIÉN QUIERES CLIPEAR?' : 'ANALIZA CUALQUIER DEMO'}
            description={
              stage === 'picking' ? (
                <>Elige a un jugador de la demo y forjaremos sus mejores jugadas en un reel.</>
              ) : (
                <>
                  Suelta un .dem — tuyo o de cualquiera — y forja sus mejores
                  jugadas en un reel. Sin login.
                </>
              )
            }
          />

          <div className="mt-7 sm:mt-8">
            {stage === 'idle' ? (
              <div className="flex flex-col gap-3">
                <DemoDropzone onFile={onFile} />
                {error ? (
                  <p role="alert" className="border border-destructive/30 bg-destructive/[0.08] px-4 py-3 text-sm text-destructive">
                    {error}
                  </p>
                ) : null}
                <ol aria-label="Cómo funciona" className="mt-2 grid gap-3 md:grid-cols-3">
                  {PIPELINE_STEPS.map((step) => (
                    <li key={step.n} className="studio-panel flex min-h-[132px] items-start gap-3.5 p-4 sm:p-5">
                      <span
                        className={`grid size-9 shrink-0 place-items-center border font-[family-name:var(--font-mono)] text-sm ${step.accent} ${step.badge}`}
                      >
                        {step.n}
                      </span>
                      <div className="min-w-0">
                        <h2 className="font-[family-name:var(--font-display)] text-sm font-bold leading-5 text-foreground">
                          {step.title}
                        </h2>
                        <p className="mt-1.5 text-[13px] leading-5 text-muted-foreground">
                          {step.copy}
                        </p>
                      </div>
                    </li>
                  ))}
                </ol>
              </div>
            ) : (
              <Card className="studio-panel-raised overflow-hidden p-4 sm:p-6">{cardContent}</Card>
            )}
          </div>
        </div>

        <footer className="flex min-h-16 items-center border-t border-border/60 py-4 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.18em] text-muted-foreground/80">
          TÚ PONES EL PC Y LA GPU · NOSOTROS EL RESTO
        </footer>
      </div>
    </main>
  );
}
