'use client';

import { useState } from 'react';
import { CheckCircle2, ExternalLink, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { useSession } from '@/lib/session';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';

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
        setError('No pudimos validar esos códigos. Revísalos e inténtalo de nuevo.');
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
          : 'No pudimos cargar tus partidas. Inténtalo de nuevo.';
      const code = (err as { code?: string } | null)?.code;
      setLinkUnavailable(code === 'steam_not_configured' || code === 'steam_unreachable');
      setError(message);
      setSubmitting(false);
    }
  }

  return (
    <div className="text-left">
      <SectionEyebrow number={1} label="VINCULA STEAM" />
      <h1 className="mt-2 font-[family-name:var(--font-display)] text-2xl font-bold uppercase tracking-tight text-foreground">
        Vincula tu historial
      </h1>
      <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
        Pega tu código de autenticación de Steam y el sharecode de tu partida más
        reciente. Los usamos para escanear tus demos en busca de highlights.
      </p>

      <form onSubmit={handleSubmit} className="mt-6 space-y-5">
        <div className="space-y-2">
          <Label
            htmlFor="auth-code"
            className="font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.14em] text-muted-foreground"
          >
            Código de autenticación
          </Label>
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
          <Label
            htmlFor="known-code"
            className="font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.14em] text-muted-foreground"
          >
            Sharecode más reciente
          </Label>
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
            Genera tu código de autenticación y copia tu sharecode más reciente desde Steam.{' '}
            <button
              type="button"
              onClick={() => setShowHelp((open) => !open)}
              aria-expanded={showHelp}
              className="font-medium text-primary hover:underline"
            >
              Cómo conseguirlos
            </button>
          </p>

          {showHelp ? (
            <div className="mt-2 space-y-2 border border-border bg-card/60 p-3">
              <p>
                <span className="font-medium text-foreground">Código de autenticación</span> — genéralo en
                la{' '}
                <a
                  href="https://help.steampowered.com/en/wizard/HelpWithGameIssue/?appid=730&issueid=128"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-primary hover:underline"
                >
                  página de autenticación de Steam
                  <ExternalLink className="size-3" aria-hidden />
                </a>{' '}
                (inicia sesión en Steam primero; el código tiene el formato <span className="font-[family-name:var(--font-mono)]">XXXX-XXXXX-XXXX</span>).
              </p>
              <p>
                <span className="font-medium text-foreground">Sharecode</span> — en CS2 abre{' '}
                <span className="font-[family-name:var(--font-mono)]">Ver → Tus partidas</span>, y copia el{' '}
                <span className="font-[family-name:var(--font-mono)]">CSGO-…</span> de tu partida más reciente.
                Con la sesión iniciada en Steam, también puedes abrir tu{' '}
                <a
                  href="https://steamcommunity.com/my/gcpd/730?tab=matchhistorypremier"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-primary hover:underline"
                >
                  historial de partidas Premier
                  <ExternalLink className="size-3" aria-hidden />
                </a>{' '}
                (o las pestañas Competitivo / Wingman) para copiar el enlace de una partida.
              </p>
            </div>
          ) : null}
        </div>

        {error ? (
          <div
            role="alert"
            className="space-y-1 border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive-foreground"
          >
            <p>{error}</p>
            {linkUnavailable ? (
              <p className="text-xs text-destructive-foreground/80">
                La vinculación del historial no está disponible ahora mismo — usa{' '}
                <span className="font-medium">Saltar por ahora</span> abajo y vincúlalo más tarde.
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
            partidas encontradas
          </Badge>
        ) : null}

        <Button
          type="submit"
          size="lg"
          className="neon-notch w-full font-[family-name:var(--font-display)] font-bold tracking-[0.06em] uppercase"
          disabled={!canSubmit}
        >
          {submitting ? (
            <>
              <Loader2 className="size-4 animate-spin" aria-hidden />
              Cargando tus partidas…
            </>
          ) : (
            'Cargar mis partidas'
          )}
        </Button>
      </form>
    </div>
  );
}
