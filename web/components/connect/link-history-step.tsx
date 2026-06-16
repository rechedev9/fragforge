'use client';

import { useState } from 'react';
import { CheckCircle2, ExternalLink, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { useSession } from '@/lib/session';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { SectionEyebrow } from '@/components/brand';

export type LinkHistoryStepProps = {
  /** Called once the history is linked, with the number of matches found. */
  onLinked: (matchesFound: number) => void;
};

/**
 * Step 1 — link the player's CS2 match history by pasting their Steam auth code
 * and most recent sharecode. On success it reports the match count and lets the
 * onboarding advance to PC pairing.
 */
export function LinkHistoryStep({ onLinked }: LinkHistoryStepProps) {
  const { refresh } = useSession();
  const [authCode, setAuthCode] = useState('');
  const [knownCode, setKnownCode] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [matchesFound, setMatchesFound] = useState<number | null>(null);

  const canSubmit =
    authCode.trim().length > 0 && knownCode.trim().length > 0 && !submitting;

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSubmit) return;

    setSubmitting(true);
    setError(null);
    try {
      const result = await api.linkMatchHistory({
        authCode: authCode.trim(),
        knownCode: knownCode.trim(),
      });
      if (!result.ok) {
        setError("We couldn't validate those codes. Check them and try again.");
        return;
      }
      setMatchesFound(result.matchesFound);
      await refresh();
      onLinked(result.matchesFound);
    } catch {
      setError('Something went wrong loading your matches. Try again.');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div>
      <SectionEyebrow label="Step 01" />
      <h2 className="mt-2 font-[family-name:var(--font-display)] text-2xl font-bold tracking-tight">
        Link your match history
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
        Paste your Steam authentication code and the sharecode of your most
        recent match. We use them to scan your demos for highlights.
      </p>

      <form onSubmit={handleSubmit} className="mt-6 space-y-5">
        <div className="space-y-2">
          <Label htmlFor="auth-code">Authentication code</Label>
          <Input
            id="auth-code"
            autoComplete="off"
            spellCheck={false}
            placeholder="XXXX-XXXXX-XXXX"
            value={authCode}
            onChange={(event) => setAuthCode(event.target.value)}
            className="font-[family-name:var(--font-mono)]"
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="known-code">Most recent sharecode</Label>
          <Input
            id="known-code"
            autoComplete="off"
            spellCheck={false}
            placeholder="CSGO-XXXXX-XXXXX-XXXXX-XXXXX-XXXXX"
            value={knownCode}
            onChange={(event) => setKnownCode(event.target.value)}
            className="font-[family-name:var(--font-mono)]"
          />
        </div>

        <p className="text-xs leading-relaxed text-muted-foreground">
          Generate your auth code and copy your latest sharecode from Steam.{' '}
          <a
            href="https://help.steampowered.com/en/wizard/HelpWithGameIssue/?appid=730&issueid=128"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 font-medium text-primary hover:underline"
          >
            How to get these
            <ExternalLink className="size-3" aria-hidden />
          </a>
        </p>

        {error ? (
          <p
            role="alert"
            className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive-foreground"
          >
            {error}
          </p>
        ) : null}

        {matchesFound !== null && !error ? (
          <Badge className="gap-1.5">
            <CheckCircle2 className="size-3" aria-hidden />
            <span className="font-[family-name:var(--font-mono)] tabular-nums">
              {matchesFound}
            </span>
            matches found
          </Badge>
        ) : null}

        <Button type="submit" size="lg" className="w-full" disabled={!canSubmit}>
          {submitting ? (
            <>
              <Loader2 className="size-4 animate-spin" aria-hidden />
              Loading my matches…
            </>
          ) : (
            'Load my matches'
          )}
        </Button>
      </form>
    </div>
  );
}
