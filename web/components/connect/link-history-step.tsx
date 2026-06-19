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
  const [linkUnavailable, setLinkUnavailable] = useState(false);
  const [matchesFound, setMatchesFound] = useState<number | null>(null);
  const [showHelp, setShowHelp] = useState(false);

  const canSubmit =
    authCode.trim().length > 0 && knownCode.trim().length > 0 && !submitting;

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSubmit) return;

    setSubmitting(true);
    setError(null);
    setLinkUnavailable(false);
    try {
      const result = await api.linkMatchHistory({
        authCode: authCode.trim(),
        knownCode: knownCode.trim(),
      });
      if (!result.ok) {
        setError("We couldn't validate those codes. Check them and try again.");
        setSubmitting(false);
        return;
      }
      setMatchesFound(result.matchesFound);
      await refresh();
      // Show the "N matches found" confirmation before advancing to step 2;
      // keep the button busy meanwhile so it can't be re-submitted.
      window.setTimeout(() => onLinked(result.matchesFound), 1200);
    } catch (err) {
      // Show the real, actionable error (the API surfaces it). Branch on the
      // backend's stable `code` — not the message text — to decide whether to
      // nudge the player toward the "Skip for now" path.
      const message =
        err instanceof Error && err.message.trim().length > 0
          ? err.message
          : 'We could not load your matches. Please try again.';
      const code = (err as { code?: string } | null)?.code;
      setLinkUnavailable(code === 'steam_not_configured' || code === 'steam_unreachable');
      setError(message);
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

        <div className="text-xs leading-relaxed text-muted-foreground">
          <p>
            Generate your auth code and copy your latest sharecode from Steam.{' '}
            <button
              type="button"
              onClick={() => setShowHelp((open) => !open)}
              aria-expanded={showHelp}
              className="font-medium text-primary hover:underline"
            >
              How to get these
            </button>
          </p>

          {showHelp ? (
            <div className="mt-2 space-y-2 rounded-md border border-border bg-card/60 p-3">
              <p>
                <span className="font-medium text-foreground">Authentication code</span> — generate it on
                Steam&apos;s{' '}
                <a
                  href="https://help.steampowered.com/en/wizard/HelpWithGameIssue/?appid=730&issueid=128"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-primary hover:underline"
                >
                  Game Authentication page
                  <ExternalLink className="size-3" aria-hidden />
                </a>{' '}
                (sign in to Steam first; the code looks like <span className="font-[family-name:var(--font-mono)]">XXXX-XXXXX-XXXX</span>).
              </p>
              <p>
                <span className="font-medium text-foreground">Sharecode</span> — in CS2 open{' '}
                <span className="font-[family-name:var(--font-mono)]">Watch → Your Matches</span>, then copy the{' '}
                <span className="font-[family-name:var(--font-mono)]">CSGO-…</span> code of your most recent match.
                Signed in to Steam, you can also open your{' '}
                <a
                  href="https://steamcommunity.com/my/gcpd/730?tab=matchhistorypremier"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-primary hover:underline"
                >
                  Premier match history
                  <ExternalLink className="size-3" aria-hidden />
                </a>{' '}
                (or the Competitive / Wingman tabs) to copy a match&apos;s share link.
              </p>
            </div>
          ) : null}
        </div>

        {error ? (
          <div
            role="alert"
            className="space-y-1 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive-foreground"
          >
            <p>{error}</p>
            {linkUnavailable ? (
              <p className="text-xs text-destructive-foreground/80">
                Match-history linking isn&apos;t available right now — use{' '}
                <span className="font-medium">Skip for now</span> below and link it later.
              </p>
            ) : null}
          </div>
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
