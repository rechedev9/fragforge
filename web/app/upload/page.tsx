'use client';

import { useCallback, useMemo, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  AlertTriangle,
  ArrowLeft,
  CheckCircle2,
  CloudUpload,
  Cog,
  FileVideo,
  Info,
  Loader2,
  LockKeyhole,
  Monitor,
  Play,
  X,
} from 'lucide-react';
import { api } from '@/lib/api';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';
import type { DemoPlayer, RosterMatch } from '@/lib/api/types';
import { aggregateSeriesRoster } from '@/lib/api/series-roster';
import { prettyMapName } from '@/lib/format';
import { navSection } from '@/lib/nav';
import { seriesTitle } from '@/lib/series-status';
import { Wordmark } from '@/components/brand/wordmark';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DemoDropzone } from '@/components/upload/demo-dropzone';
import { PlayerPicker } from '@/components/upload/player-picker';

const NAV = navSection('/upload');

/**
 * The pipeline stage the upload flow is in. `scanning`/`parsing` render either a
 * single centered spinner (one demo) or a per-map row list (a series); the
 * `seriesMode` flag, not the stage, decides which.
 */
type Stage = 'idle' | 'scanning' | 'picking' | 'parsing';

/**
 * One dropped demo's roster-scan state; scanned rows carry the job + roster. An
 * error row may carry a `reason`: a specific hint (e.g. a demo that scanned but
 * yielded zero players) shown in place of the generic "Error" so the user can
 * tell a bad demo apart from a transient failure.
 */
type ScanRow =
  | { fileName: string; status: 'scanning' }
  | { fileName: string; status: 'scanned'; jobId: string; players: DemoPlayer[]; match?: RosterMatch }
  | { fileName: string; status: 'error'; reason?: string };

/** One scanned demo's parse state after the player is picked (series mode). */
type ParseRow = { jobId: string; label: string; status: 'parsing' | 'done' | 'skipped' | 'error' };

/**
 * Hint for a demo that scanned without errors yet yielded an empty roster (e.g.
 * a Source-1 demo that passes the header checks but parses to zero players), so
 * its error row reads as "wrong demo" rather than a transient failure.
 */
const ZERO_PLAYERS_HINT = 'Sin jugadores — ¿seguro que es una demo de CS2?';

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

/** A scanned demo's short label: prettified map name, else its file name. */
function rowLabel(row: Extract<ScanRow, { status: 'scanned' }>): string {
  return row.match ? prettyMapName(row.match.map) : row.fileName;
}

/** The three pipeline steps under the dropzone (mockup 2b): static copy. */
const PIPELINE_STEPS = [
  {
    n: '01',
    icon: Cog,
    accent: 'text-primary',
    badge: 'border-primary/35 bg-primary/10',
    title: 'ANÁLISIS AUTOMÁTICO',
    copy: 'Parseamos la demo y puntuamos cada ronda: clutches, aces, multi-kills.',
  },
  {
    n: '02',
    icon: Play,
    accent: 'text-primary',
    badge: 'border-primary/35 bg-primary/10',
    title: 'ELIGES LAS JUGADAS',
    copy: 'Filmstrip con las mejores jugadas detectadas. Marca las que quieres en el reel.',
  },
  {
    n: '03',
    icon: Monitor,
    accent: 'text-primary',
    badge: 'border-primary/35 bg-primary/10',
    title: 'RENDER EN TU RIG',
    copy: 'Captura y edición en tu propio PC. 9:16 para Shorts o 16:9 para largo.',
  },
] as const;

/**
 * Upload flow (/upload) — the no-login entry. Drop one .dem (yours or someone
 * else's) or several at once for a whole bo3/bo5 series. We scan every roster,
 * let you pick whose POV to clip (aggregated across maps for a series), then
 * parse that player on each map. A single demo routes into its match; a series
 * routes into the series view. Renders on the root layout (no sidebar).
 */
export default function UploadPage() {
  const router = useRouter();
  const homeHref = '/matches';
  const [stage, setStage] = useState<Stage>('idle');
  const [seriesId, setSeriesId] = useState<string | null>(null);

  // Single-demo state (seriesMode === false).
  const [fileName, setFileName] = useState<string | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const [players, setPlayers] = useState<DemoPlayer[]>([]);
  const [match, setMatch] = useState<RosterMatch | null>(null);

  // Series state (seriesMode === true).
  const [scanRows, setScanRows] = useState<ScanRow[]>([]);
  const [parseRows, setParseRows] = useState<ParseRow[]>([]);

  // Series mode is fully derived from the scan rows: a series scan seeds them
  // (2+ demos), the single-demo path never does, and reset clears them. There is
  // no independent flag to drift out of sync with the rows.
  const seriesMode = scanRows.length > 0;

  const [error, setError] = useState<string | null>(null);
  const [warning, setWarning] = useState<string | null>(null);

  const reset = useCallback((message: string | null) => {
    setError(message);
    setWarning(null);
    setStage('idle');
    setSeriesId(null);
    setFileName(null);
    setJobId(null);
    setPlayers([]);
    setMatch(null);
    setScanRows([]);
    setParseRows([]);
  }, []);

  // --- Single-demo flow (identical outcome to the pre-series behaviour) ---

  const runScan = useCallback(
    async (file: File) => {
      setError(null);
      setWarning(null);
      setFileName(file.name);
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
        reset(
          isServiceUnavailable(err)
            ? 'El servicio de análisis está offline. Arráncalo y vuelve a intentarlo.'
            : 'No se pudo escanear esa demo. Prueba con otro archivo .dem.',
        );
      }
    },
    [reset],
  );

  const onPickSingle = useCallback(
    async (steamId: string) => {
      if (stage !== 'picking' || seriesMode || !jobId) return;
      setError(null);
      setStage('parsing');
      try {
        const parsed = await api.parseDemo({ jobId, steamId });
        router.push('/matches/' + parsed.id);
      } catch (err) {
        reset(
          isServiceUnavailable(err)
            ? 'El servicio de análisis está offline. Arráncalo y vuelve a intentarlo.'
            : 'No se pudieron extraer los highlights de ese jugador. Elige otro.',
        );
      }
    },
    [stage, seriesMode, jobId, router, reset],
  );

  // --- Series flow (2+ demos dropped) ---

  const runSeriesScan = useCallback(
    async (files: File[]) => {
      const sid = crypto.randomUUID();
      setError(null);
      setWarning(null);
      setSeriesId(sid);
      setScanRows(files.map((f) => ({ fileName: f.name, status: 'scanning' })));
      setStage('scanning');

      let sawOffline = false;
      const settle = files.map((file, i) =>
        api
          .scanDemo(file, { seriesId: sid })
          .then((scan): ScanRow => {
            if (scan.players.length === 0) return { fileName: file.name, status: 'error', reason: ZERO_PLAYERS_HINT };
            const row: ScanRow = { fileName: file.name, status: 'scanned', jobId: scan.jobId, players: scan.players };
            if (scan.match) row.match = scan.match;
            return row;
          })
          .catch((err): ScanRow => {
            // One demo's rejection must never sink the others: swallow it here
            // and surface it as a failed row (and the shared offline flag).
            if (isServiceUnavailable(err)) sawOffline = true;
            return { fileName: file.name, status: 'error' };
          })
          .then((row) => {
            // Land each result as it settles so rows resolve live, not in a batch.
            setScanRows((prev) => {
              const next = [...prev];
              next[i] = row;
              return next;
            });
            return row;
          }),
      );

      const rows = await Promise.all(settle);
      const scanned = rows.filter((r) => r.status === 'scanned');
      const failed = rows.filter((r) => r.status === 'error');

      if (scanned.length === 0) {
        reset(
          sawOffline
            ? 'El servicio de análisis está offline. Arráncalo y vuelve a intentarlo.'
            : 'No se pudo escanear ninguna de las demos. Prueba con otros archivos .dem.',
        );
        return;
      }
      if (failed.length > 0) {
        setWarning(
          `No se pudieron escanear ${failed.length} de ${rows.length} demos: ${failed.map((r) => r.fileName).join(', ')}.`,
        );
      }
      setStage('picking');
    },
    [reset],
  );

  const scannedRows = useMemo(
    () => scanRows.filter((r): r is Extract<ScanRow, { status: 'scanned' }> => r.status === 'scanned'),
    [scanRows],
  );
  const aggregated = useMemo(() => aggregateSeriesRoster(scannedRows.map((r) => r.players)), [scannedRows]);

  const onPickSeries = useCallback(
    async (steamId: string) => {
      if (stage !== 'picking' || !seriesMode || !seriesId) return;
      setError(null);
      const rows: ParseRow[] = scannedRows.map((r) => {
        const hasPlayer = r.players.some((p) => p.steamId === steamId);
        return { jobId: r.jobId, label: rowLabel(r), status: hasPlayer ? 'parsing' : 'skipped' };
      });
      setParseRows(rows);
      setStage('parsing');

      await Promise.allSettled(
        rows.map(async (row, i) => {
          if (row.status === 'skipped') return;
          const next: ParseRow['status'] = await api
            .parseDemo({ jobId: row.jobId, steamId })
            .then((): ParseRow['status'] => 'done')
            .catch((): ParseRow['status'] => 'error');
          setParseRows((prev) => {
            const copy = [...prev];
            copy[i] = { ...copy[i], status: next };
            return copy;
          });
        }),
      );

      // Navigate regardless of per-map failures: the series view shows each
      // demo's status (ready / failed) and lets the user forge the ones that
      // parsed.
      router.push('/series/' + seriesId);
    },
    [stage, seriesMode, seriesId, scannedRows, router],
  );

  const onFiles = useCallback(
    (files: File[]) => {
      if (stage !== 'idle' || files.length === 0) return;
      // A single demo keeps the original single-match experience; 2+ demos run
      // the series flow (and mint the shared series id).
      if (files.length === 1) void runScan(files[0]);
      else void runSeriesScan(files);
    },
    [stage, runScan, runSeriesScan],
  );

  // --- Header copy ---

  // Reachable singular: 2+ demos dropped but only one scan survived.
  const mapCount = scannedRows.length;
  let headerLabel = NAV.label.toUpperCase();
  let headerTitle = 'ANALIZA CUALQUIER DEMO';
  let headerDescription: ReactNode = (
    <>Suelta un .dem — o varios, una serie bo3/bo5 completa — y forja las mejores jugadas en un reel. Sin login.</>
  );
  if (seriesMode) {
    headerLabel = 'SERIE';
    if (stage === 'scanning') {
      headerTitle = 'ANALIZANDO LA SERIE';
      headerDescription = <>Escaneando {scanRows.length} demos de la serie…</>;
    } else if (stage === 'picking') {
      headerTitle = seriesTitle(mapCount);
      headerDescription = (
        <>Elige un jugador y forjaremos sus mejores jugadas en {scannedRows.map(rowLabel).join(', ')}.</>
      );
    } else if (stage === 'parsing') {
      headerTitle = seriesTitle(mapCount);
      headerDescription =
        mapCount === 1 ? (
          <>Forjando los highlights del jugador en el mapa de la serie…</>
        ) : (
          <>Forjando los highlights del jugador en cada mapa de la serie…</>
        );
    }
  } else if (stage === 'picking') {
    headerTitle = '¿A QUIÉN QUIERES CLIPEAR?';
    headerDescription = <>Elige a un jugador de la demo y forjaremos sus mejores jugadas en un reel.</>;
  }

  // --- Card content ---

  let cardContent: ReactNode;
  if (seriesMode && stage === 'scanning') {
    cardContent = <ScanRowList rows={scanRows} />;
  } else if (seriesMode && stage === 'parsing') {
    cardContent = <ParseRowList rows={parseRows} />;
  } else if (seriesMode && stage === 'picking') {
    cardContent = <PlayerPicker players={aggregated} onPick={onPickSeries} seriesMapCount={mapCount} />;
  } else if (stage === 'scanning' || stage === 'parsing') {
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
    cardContent = <PlayerPicker players={players} onPick={onPickSingle} match={match ?? undefined} />;
  } else {
    // stage === 'idle': not rendered (the dropzone shows instead), so this
    // branch only exists to keep cardContent exhaustively assigned.
    cardContent = null;
  }

  return (
    <main className="relative min-h-screen overflow-x-hidden">
      {/* Ambient light keeps the standalone upload entry visually connected to
          the Studio shell without competing with the working surface. */}
      <div
        aria-hidden
        className="pointer-events-none absolute -top-52 left-1/2 h-[40rem] w-[48rem] -translate-x-1/2"
        style={{
          background:
            'radial-gradient(ellipse at center, color-mix(in oklch, var(--primary) 9%, transparent), transparent 70%)',
        }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute top-[38rem] -right-40 size-[28rem]"
        style={{
          background:
            'radial-gradient(circle at center, color-mix(in oklch, var(--stream) 3.5%, transparent), transparent 70%)',
        }}
      />

      <div className="relative mx-auto flex min-h-screen w-full max-w-[1536px] flex-col px-4 sm:px-6 lg:px-12">
        <header className="relative flex min-h-[68px] items-center justify-between border-b border-border/60 py-3">
          <Link href={homeHref} aria-label="Inicio de FragForge">
            <Wordmark />
          </Link>
          <div
            aria-current="step"
            className="absolute inset-y-0 left-1/2 hidden -translate-x-1/2 items-center border-b-2 border-primary px-5 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.18em] text-primary md:flex"
          >
            <CloudUpload className="mr-3 size-4" />
            {NAV.number} — {NAV.label}
          </div>
          <Button variant="ghost" size="sm" asChild className="text-muted-foreground hover:text-foreground">
            <Link href={homeHref}>
              <ArrowLeft className="size-4" />
              Volver
            </Link>
          </Button>
        </header>

        <div className="flex flex-1 flex-col py-8 sm:py-10 lg:py-9">
          <StudioPageHeader number={Number(NAV.number)} label={headerLabel} title={headerTitle} description={headerDescription} />

          <div className="mt-7 sm:mt-8">
            {stage === 'idle' ? (
              <div className="flex flex-col gap-3">
                <DemoDropzone onFiles={onFiles} />
                {error ? (
                  <p role="alert" className="border border-destructive/30 bg-destructive/[0.08] px-4 py-3 text-sm text-destructive">
                    {error}
                  </p>
                ) : null}
                <ol aria-label="Cómo funciona" className="mt-2 grid gap-4 lg:grid-cols-3">
                  {PIPELINE_STEPS.map((step, index) => {
                    const StepIcon = step.icon;
                    return (
                      <li
                        key={step.n}
                        className="studio-panel studio-panel-interactive relative flex min-h-[150px] items-start gap-4 p-5 sm:p-6"
                      >
                        <span
                          className={`grid size-12 shrink-0 place-items-center border font-[family-name:var(--font-mono)] text-base ${step.accent} ${step.badge}`}
                        >
                          {step.n}
                        </span>
                        <div className="min-w-0">
                          <h2 className="flex items-center gap-2.5 font-[family-name:var(--font-display)] text-base font-bold leading-6 text-foreground">
                            <StepIcon className="size-5 text-primary" strokeWidth={1.7} />
                            {step.title}
                          </h2>
                          <p className="mt-2 text-sm leading-6 text-muted-foreground">{step.copy}</p>
                        </div>
                        {index < PIPELINE_STEPS.length - 1 ? (
                          <span
                            aria-hidden
                            className="absolute -right-[17px] top-1/2 z-10 hidden -translate-y-1/2 bg-background px-1 font-[family-name:var(--font-mono)] text-primary xl:block"
                          >
                            →
                          </span>
                        ) : null}
                      </li>
                    );
                  })}
                </ol>
              </div>
            ) : (
              <div className="flex flex-col gap-3">
                {warning ? (
                  <div
                    role="alert"
                    className="flex items-start justify-between gap-3 border border-amber-400/30 bg-amber-400/[0.08] px-4 py-3 text-sm text-amber-400"
                  >
                    <span className="min-w-0">{warning}</span>
                    <button
                      type="button"
                      aria-label="Descartar aviso"
                      onClick={() => setWarning(null)}
                      className="shrink-0 text-amber-400/70 transition-colors hover:text-amber-400"
                    >
                      <X className="size-4" />
                    </button>
                  </div>
                ) : null}
                <Card className="studio-panel-raised overflow-hidden p-4 sm:p-6">{cardContent}</Card>
              </div>
            )}
          </div>
        </div>

        <footer className="flex min-h-16 flex-wrap items-center justify-between gap-3 border-t border-border/60 py-4 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.14em] text-muted-foreground">
          <span className="inline-flex items-center gap-3">
            <LockKeyhole className="size-4 text-primary" />
            <strong className="font-normal text-primary">Procesamiento local y privado</strong>
            <span className="hidden text-border sm:inline">|</span>
            <span className="hidden sm:inline">Tus archivos, tu equipo, tu control.</span>
          </span>
          <span className="inline-flex items-center gap-3">
            <Info className="size-4" />
            Formato: .dem
            <span className="text-border">|</span>
            Máx. 10 demos
          </span>
        </footer>
      </div>
    </main>
  );
}

/** The per-demo roster-scan progress list shown while a series is scanning. */
function ScanRowList({ rows }: { rows: ScanRow[] }) {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-2">
      <p className="mb-1 font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-wider text-muted-foreground">
        Escaneando {rows.length} demos
      </p>
      {rows.map((row, i) => (
        <div
          key={`${row.fileName}-${i}`}
          className="flex items-center justify-between gap-3 border border-primary/15 bg-muted/20 px-3.5 py-2.5"
        >
          <span className="flex min-w-0 items-center gap-2 font-[family-name:var(--font-mono)] text-sm text-foreground">
            <FileVideo className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate">{row.fileName}</span>
          </span>
          <span className="flex shrink-0 items-center gap-2 text-sm">
            {row.status === 'scanning' ? (
              <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                Escaneando…
              </span>
            ) : null}
            {row.status === 'scanned' ? (
              <span className="inline-flex items-center gap-2">
                {row.match ? (
                  <span className="inline-flex items-center gap-1.5 font-[family-name:var(--font-mono)] text-xs">
                    <span className="font-[family-name:var(--font-display)] font-bold uppercase tracking-wide text-foreground">
                      {prettyMapName(row.match.map)}
                    </span>
                    <span className="tabular-nums text-muted-foreground">
                      {row.match.scoreT}-{row.match.scoreCt}
                    </span>
                  </span>
                ) : (
                  <span className="text-muted-foreground">Escaneada</span>
                )}
                <CheckCircle2 className="size-4 text-primary" />
              </span>
            ) : null}
            {row.status === 'error' ? (
              <span
                className="inline-flex max-w-[13rem] items-center gap-1.5 text-destructive sm:max-w-xs"
                title={row.reason}
              >
                <AlertTriangle className="size-4 shrink-0" />
                <span className="truncate">{row.reason ?? 'Error'}</span>
              </span>
            ) : null}
          </span>
        </div>
      ))}
    </div>
  );
}

/** The per-map parse progress list shown after the player is picked (series). */
function ParseRowList({ rows }: { rows: ParseRow[] }) {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-2">
      <p className="mb-1 font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-wider text-muted-foreground">
        Forjando highlights en cada mapa
      </p>
      {rows.map((row, i) => (
        <div
          key={`${row.jobId}-${i}`}
          className="flex items-center justify-between gap-3 border border-primary/15 bg-muted/20 px-3.5 py-2.5"
        >
          <span className="min-w-0 truncate font-[family-name:var(--font-display)] text-sm font-bold uppercase tracking-wide text-foreground">
            {row.label}
          </span>
          <span className="flex shrink-0 items-center gap-1.5 text-sm">
            {row.status === 'parsing' ? (
              <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                Analizando…
              </span>
            ) : null}
            {row.status === 'done' ? (
              <span className="inline-flex items-center gap-1.5 text-primary">
                <CheckCircle2 className="size-4" />
                Lista
              </span>
            ) : null}
            {row.status === 'skipped' ? (
              <span className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-muted-foreground/70">
                sin este jugador
              </span>
            ) : null}
            {row.status === 'error' ? (
              <span className="inline-flex items-center gap-1.5 text-destructive">
                <AlertTriangle className="size-4" />
                Error
              </span>
            ) : null}
          </span>
        </div>
      ))}
    </div>
  );
}
