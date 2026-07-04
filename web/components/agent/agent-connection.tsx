'use client';

import { useCallback, useEffect, useState } from 'react';
import { Cpu, Loader2, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';
import {
  DEFAULT_AGENT_URL,
  agentBaseUrl,
  agentToken,
  setAgentUrl,
  setAgentToken,
  probeAgent,
  type AgentProbeResult,
} from '@/lib/agent/connection';

// The FragForge Agent Windows installer, the same canonical release asset the
// landing page's download CTA points at. Kept as a literal here since web/ and
// landing/ are separate Next apps with no shared config; bump both in lockstep
// on a new release.
const AGENT_DOWNLOAD_URL =
  'https://github.com/rechedev9/fragforge/releases/download/v0.2.13/FragForge.Studio.Setup.0.2.13.exe';

type ConnState = 'checking' | 'connected' | 'disconnected';

/**
 * Shared probe hook: reads the persisted URL/token into local form state, probes
 * the agent on mount and on demand, and exposes save + retry. In hosted mode the
 * browser calls the local agent directly, so "connected" means the agent
 * answered a token-authenticated probe (see lib/agent/connection.probeAgent).
 */
function useAgentConnection() {
  const [url, setUrl] = useState(DEFAULT_AGENT_URL);
  const [token, setToken] = useState('');
  const [state, setState] = useState<ConnState>('checking');
  const [reason, setReason] = useState<string | undefined>(undefined);

  // Hydrate from localStorage once mounted (SSR-safe: only runs in the browser).
  useEffect(() => {
    setUrl(agentBaseUrl());
    setToken(agentToken());
  }, []);

  const probe = useCallback(async () => {
    setState('checking');
    let result: AgentProbeResult;
    try {
      result = await probeAgent(agentBaseUrl());
    } catch {
      result = { connected: false, reason: 'probe failed' };
    }
    setState(result.connected ? 'connected' : 'disconnected');
    setReason(result.reason);
  }, []);

  useEffect(() => {
    void probe();
  }, [probe]);

  /** Persists the current form values, then re-probes with them. */
  const save = useCallback(async () => {
    setAgentUrl(url);
    setAgentToken(token);
    await probe();
  }, [url, token, probe]);

  return { url, setUrl, token, setToken, state, reason, probe, save };
}

/** Per-state presentation for the connection dot + label. */
const DOT_TONE: Record<ConnState, string> = {
  connected: 'text-primary',
  checking: 'text-muted-foreground',
  disconnected: 'text-destructive',
};
const DOT_MARK: Record<ConnState, string> = {
  connected: 'bg-primary shadow-[0_0_8px_var(--primary)]',
  checking: 'bg-muted-foreground',
  disconnected: 'bg-destructive shadow-[0_0_8px_var(--destructive)] neon-pulse',
};
const DOT_LABEL: Record<ConnState, string> = {
  connected: 'Agente conectado',
  checking: 'Comprobando…',
  disconnected: 'Agente desconectado',
};

/** A small colored dot + label reflecting the connection state. */
function StatusDot({ state }: { state: ConnState }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.16em]',
        DOT_TONE[state],
      )}
    >
      <span className={cn('size-[7px] rounded-full', DOT_MARK[state])} />
      {DOT_LABEL[state]}
    </span>
  );
}

/**
 * The full "Agent connection" panel: connection status, editable agent URL +
 * pairing token, a save/retry action, and - when disconnected - clear
 * instructions to download and run the FragForge Agent. Used in onboarding
 * (pair-pc-step) in hosted mode.
 */
export function AgentConnection({ className }: { className?: string }) {
  const { url, setUrl, token, setToken, state, reason, probe, save } = useAgentConnection();
  const [saving, setSaving] = useState(false);

  async function handleSave() {
    setSaving(true);
    try {
      await save();
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className={cn('text-left', className)}>
      <div className="flex items-center justify-between gap-3">
        <StatusDot state={state} />
        <Button variant="ghost" size="sm" onClick={() => void probe()} disabled={state === 'checking'} className="gap-1.5">
          <RefreshCw className={cn('size-3.5', state === 'checking' && 'animate-spin')} aria-hidden />
          Reintentar
        </Button>
      </div>

      <div className="mt-5 grid gap-4">
        <div className="grid gap-1.5">
          <Label htmlFor="ff-agent-url" className="font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
            URL del agente
          </Label>
          <Input
            id="ff-agent-url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder={DEFAULT_AGENT_URL}
            autoComplete="off"
            spellCheck={false}
            inputMode="url"
          />
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="ff-agent-token" className="font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
            Token de emparejamiento
          </Label>
          <Input
            id="ff-agent-token"
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Pega el token que imprime el agente al arrancar"
            autoComplete="off"
            spellCheck={false}
          />
          <p className="text-xs leading-relaxed text-muted-foreground/80">
            El agente imprime su token al iniciarse y lo guarda en{' '}
            <span className="font-[family-name:var(--font-mono)]">agent-pairing.token</span>. Pégalo aquí; se
            guarda solo en este navegador y viaja en la cabecera X-FragForge-Token.
          </p>
        </div>

        <Button onClick={handleSave} disabled={saving} className="w-full font-[family-name:var(--font-display)] font-semibold tracking-[0.05em]">
          {saving ? <Loader2 className="size-4 animate-spin" aria-hidden /> : <Cpu className="size-4" aria-hidden />}
          Guardar y conectar
        </Button>
      </div>

      {state === 'disconnected' ? (
        <div className="mt-6 border border-border bg-card/50 p-4">
          <p className="text-sm font-medium text-foreground">No se encuentra el agente FragForge</p>
          <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
            {reason ? `${reason}. ` : ''}
            Descarga el agente, ejecútalo en este PC y copia aquí el token que imprime al arrancar. Todo el
            procesado y los vídeos se quedan en tu máquina.
          </p>
          <ol className="mt-3 space-y-1.5 text-sm text-muted-foreground">
            <li>1 · Descarga y ejecuta el agente FragForge.</li>
            <li>2 · Copia el token que imprime en la consola al arrancar.</li>
            <li>3 · Pégalo arriba y pulsa &quot;Guardar y conectar&quot;.</li>
          </ol>
          <a
            href={AGENT_DOWNLOAD_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="neon-notch mt-4 inline-flex items-center bg-primary px-5 py-2 font-[family-name:var(--font-display)] text-[13px] font-bold tracking-[0.05em] text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Descargar agente (Windows)
          </a>
        </div>
      ) : null}
    </div>
  );
}

/**
 * A compact status pill for the app shell: just the live connected/disconnected
 * dot with a manual retry, so the state is always visible without the full form.
 */
export function AgentConnectionPill({ className }: { className?: string }) {
  const { state, probe } = useAgentConnection();
  return (
    <button
      type="button"
      onClick={() => void probe()}
      title="Estado del agente FragForge (pulsa para reintentar)"
      className={cn(
        'inline-flex w-full items-center justify-between gap-2 border border-border bg-card/50 px-3 py-2 text-left transition-colors hover:bg-card',
        className,
      )}
    >
      <StatusDot state={state} />
      <RefreshCw className={cn('size-3.5 text-muted-foreground', state === 'checking' && 'animate-spin')} aria-hidden />
    </button>
  );
}
