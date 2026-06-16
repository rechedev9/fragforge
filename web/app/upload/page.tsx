'use client';

import { useCallback, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { ArrowLeft, FileVideo, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { Wordmark } from '@/components/brand';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DemoDropzone } from '@/components/upload/demo-dropzone';

/**
 * Upload flow (/upload) — the no-login entry. Drop any .dem (yours or someone
 * else's), we parse it into a match, then route into the same highlight →
 * render pipeline as Steam matches. Renders on the root layout (no sidebar):
 * the user isn't necessarily signed in here.
 */
export default function UploadPage() {
  const router = useRouter();
  const [parsing, setParsing] = useState(false);
  const [fileName, setFileName] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const onFile = useCallback(
    async (file: File) => {
      if (parsing) return;
      setError(null);
      setFileName(file.name);
      setParsing(true);
      try {
        const match = await api.uploadDemo({ fileName: file.name });
        router.push('/matches/' + match.id);
      } catch {
        setFileName(null);
        setError('Could not parse that demo. Try another .dem file.');
        setParsing(false);
      }
    },
    [parsing, router],
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
              Analyze any demo
            </h1>
            <p className="mt-3 text-muted-foreground">
              Drop a .dem file — yours or someone else&apos;s — and forge its
              best plays into a reel. No Steam login required.
            </p>
          </div>

          <Card className="overflow-hidden p-6 sm:p-8">
            {parsing ? (
              <div className="flex flex-col items-center justify-center gap-4 py-14 text-center">
                <Loader2 className="size-8 animate-spin text-primary" />
                <div className="flex flex-col gap-1">
                  <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
                    Parsing demo…
                  </p>
                  {fileName ? (
                    <p className="inline-flex items-center justify-center gap-1.5 font-[family-name:var(--font-mono)] text-sm text-muted-foreground">
                      <FileVideo className="size-4" />
                      {fileName}
                    </p>
                  ) : null}
                </div>
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
