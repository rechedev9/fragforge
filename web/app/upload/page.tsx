'use client';

import { useCallback, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { ArrowLeft, FileVideo, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';
import type { DemoPlayer } from '@/lib/api/types';
import { Wordmark } from '@/components/brand';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DemoDropzone } from '@/components/upload/demo-dropzone';
import { PlayerPicker } from '@/components/upload/player-picker';

type Stage = 'idle' | 'scanning' | 'picking' | 'parsing' | 'waiting-for-pc';

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

/**
 * Upload flow (/upload) — the no-login entry. Drop any .dem (yours or someone
 * else's), we scan its roster, let you pick whose POV to clip, then parse that
 * player into a match and route into the same highlight → render pipeline as
 * Steam matches. Renders on the root layout (no sidebar): the user isn't
 * necessarily signed in here.
 */
export default function UploadPage() {
  const router = useRouter();
  const [stage, setStage] = useState<Stage>('idle');
  const [fileName, setFileName] = useState<string | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const [players, setPlayers] = useState<DemoPlayer[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);

  const reset = useCallback((message: string) => {
    setError(message);
    setStage('idle');
    setFileName(null);
    setJobId(null);
    setPlayers([]);
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
            ? 'Analysis service is offline. Start it and try again.'
            : 'Could not scan that demo. Try another .dem file.',
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
            ? 'Analysis service is offline. Start it and try again.'
            : 'Could not parse highlights for that player. Pick another.',
        );
      }
    },
    [stage, jobId, router, reset],
  );

  return (
    <main className="relative min-h-screen overflow-hidden">
      {/* Faint lime glow, matching the onboarding screen. */}
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 left-1/2 h-[36rem] w-[36rem] -translate-x-1/2 rounded-full bg-primary/10 blur-[160px]"
      />

      <div className="relative mx-auto flex min-h-screen max-w-3xl flex-col px-6">
        <header className="flex h-16 items-center justify-between">
          <Link href="/" aria-label="FragForge home">
            <Wordmark />
          </Link>
          <Button variant="ghost" size="sm" asChild className="text-muted-foreground">
            <Link href="/">
              <ArrowLeft className="size-4" />
              Back
            </Link>
          </Button>
        </header>

        <div className="flex flex-1 flex-col justify-center py-12">
          <div className="mb-8 max-w-xl">
            <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold uppercase tracking-tight sm:text-4xl">
              {stage === 'picking' ? 'Who do you want to clip?' : 'Analyze any demo'}
            </h1>
            <p className="mt-3 text-muted-foreground">
              {stage === 'picking' ? (
                <>Pick a player from the demo and we&apos;ll forge their best plays into a reel.</>
              ) : (
                <>
                  Drop a .dem file — yours or someone else&apos;s — and forge its
                  best plays into a reel. No Steam login required.
                </>
              )}
            </p>
          </div>

          <Card className="overflow-hidden p-6 sm:p-8">
            {stage === 'scanning' || stage === 'parsing' ? (
              <div className="flex flex-col items-center justify-center gap-4 py-14 text-center">
                <Loader2 className="size-8 animate-spin text-primary" />
                <div className="flex flex-col gap-1">
                  <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
                    {stage === 'scanning' ? 'Scanning roster…' : 'Forging highlights…'}
                  </p>
                  {fileName ? (
                    <p className="inline-flex items-center justify-center gap-1.5 font-[family-name:var(--font-mono)] text-sm text-muted-foreground">
                      <FileVideo className="size-4" />
                      {fileName}
                    </p>
                  ) : null}
                </div>
              </div>
            ) : stage === 'picking' ? (
              <PlayerPicker players={players} onPick={onPick} />
            ) : stage === 'waiting-for-pc' ? (
              <div className="flex flex-col items-center justify-center gap-4 py-14 text-center">
                <div className="flex flex-col gap-1">
                  <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
                    Your PC is offline
                  </p>
                  <p className="text-sm text-muted-foreground">
                    Open FragForge Agent on your computer to analyze this demo, then retry.
                  </p>
                  {fileName ? (
                    <p className="inline-flex items-center justify-center gap-1.5 font-[family-name:var(--font-mono)] text-sm text-muted-foreground">
                      <FileVideo className="size-4" />
                      {fileName}
                    </p>
                  ) : null}
                </div>
                <Button
                  onClick={() => {
                    if (pendingFile) void runScan(pendingFile);
                  }}
                >
                  Retry
                </Button>
              </div>
            ) : (
              <div className="flex flex-col gap-3">
                <DemoDropzone onFile={onFile} />
                {error ? <p className="text-sm text-destructive">{error}</p> : null}
              </div>
            )}
          </Card>
        </div>

        <footer className="flex h-16 items-center text-xs text-muted-foreground/70">
          You bring the PC &amp; GPU. We handle the rest.
        </footer>
      </div>
    </main>
  );
}
