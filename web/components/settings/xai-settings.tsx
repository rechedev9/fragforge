'use client';

import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { CircleCheck, KeyRound, LoaderCircle, RefreshCw, Trash2, TriangleAlert, WifiOff } from 'lucide-react';
import { toast } from 'sonner';
import {
  getDesktopSettingsBridge,
  XAI_KEY_SOURCES,
  type DesktopSettingsBridge,
  type XAIConnectionTestResult,
  type XAIKeySource,
  type XAISettingsStatus,
} from '@/lib/desktop-settings';
import { Button } from '@/components/ui/button';
import { DesktopOnlyCard } from '@/components/settings/desktop-only-card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

const MAX_XAI_API_KEY_LENGTH = 4096;

const ACTIONS = {
  load: 'load',
  test: 'test',
  save: 'save',
  remove: 'remove',
  restart: 'restart',
} as const;

type Action = typeof ACTIONS[keyof typeof ACTIONS];

const SOURCE_LABELS: Record<XAIKeySource, string> = {
  [XAI_KEY_SOURCES.environment]: 'Variable de entorno',
  [XAI_KEY_SOURCES.stored]: 'Ajustes de Windows',
  [XAI_KEY_SOURCES.team]: 'Edición interna',
  [XAI_KEY_SOURCES.none]: 'Sin configurar',
};

/** xAI credential settings backed exclusively by the Electron preload bridge. */
export function XAISettings(): ReactNode {
  const [bridge, setBridge] = useState<DesktopSettingsBridge | null>();
  const [status, setStatus] = useState<XAISettingsStatus | null>(null);
  const [apiKey, setAPIKey] = useState('');
  const [action, setAction] = useState<Action | null>(ACTIONS.load);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<XAIConnectionTestResult | null>(null);

  const refreshStatus = useCallback(async (desktopBridge: DesktopSettingsBridge): Promise<void> => {
    try {
      setStatus(await desktopBridge.getXAIStatus());
      setError(null);
    } catch {
      setError('No se pudo leer la configuración protegida de xAI.');
    }
  }, []);

  useEffect(() => {
    const desktopBridge = getDesktopSettingsBridge();
    setBridge(desktopBridge);
    if (desktopBridge === null) {
      setAction(null);
      return;
    }

    let mounted = true;
    desktopBridge.getXAIStatus()
      .then((nextStatus) => {
        if (mounted) setStatus(nextStatus);
      })
      .catch(() => {
        if (mounted) setError('No se pudo leer la configuración protegida de xAI.');
      })
      .finally(() => {
        if (mounted) setAction(null);
      });
    return () => {
      mounted = false;
    };
  }, []);

  const runTest = async (): Promise<void> => {
    if (bridge === null || bridge === undefined) return;
    const candidate = apiKey.trim();
    if (candidate === '') {
      setError('Introduce una clave de xAI para probarla.');
      return;
    }
    setAction(ACTIONS.test);
    setError(null);
    setTestResult(null);
    try {
      const result = await bridge.testXAIKey(candidate);
      setTestResult(redactTestResult(result, candidate));
    } catch {
      setError('No se pudo probar la conexión con xAI.');
    } finally {
      setAction(null);
    }
  };

  const save = async (): Promise<void> => {
    if (bridge === null || bridge === undefined) return;
    const candidate = apiKey.trim();
    if (candidate === '') {
      setError('Introduce una clave de xAI para guardarla.');
      return;
    }
    setAction(ACTIONS.save);
    setError(null);
    try {
      const result = await bridge.saveXAIKey(candidate);
      if (!result.ok) {
        setError(result.error ?? 'No se pudo guardar la clave de xAI.');
        return;
      }
      setAPIKey('');
      setTestResult(null);
      if (result.status) {
        setStatus(result.status);
      } else {
        await refreshStatus(bridge);
      }
      toast('Clave de xAI guardada de forma protegida.');
    } catch {
      setError('No se pudo guardar la clave de xAI.');
    } finally {
      setAction(null);
    }
  };

  const remove = async (): Promise<void> => {
    if (bridge === null || bridge === undefined) return;
    setAction(ACTIONS.remove);
    setError(null);
    try {
      const result = await bridge.removeXAIKey();
      if (!result.ok) {
        setError(result.error ?? 'No se pudo eliminar la clave guardada.');
        return;
      }
      setAPIKey('');
      setTestResult(null);
      if (result.status) {
        setStatus(result.status);
      } else {
        await refreshStatus(bridge);
      }
      toast('Clave guardada eliminada. Reinicia FragForge para aplicar el cambio.');
    } catch {
      setError('No se pudo eliminar la clave guardada.');
    } finally {
      setAction(null);
    }
  };

  const restart = async (): Promise<void> => {
    if (bridge === null || bridge === undefined) return;
    const confirmed = window.confirm(
      'Reiniciar FragForge detendrá las grabaciones y renders que estén en curso. ¿Quieres continuar?',
    );
    if (!confirmed) return;
    setAction(ACTIONS.restart);
    setError(null);
    try {
      const result = await bridge.restartStudio();
      if (!result.ok) {
        setError(result.error ?? 'No se pudo reiniciar FragForge Studio.');
        setAction(null);
      }
    } catch {
      setError('No se pudo reiniciar FragForge Studio.');
      setAction(null);
    }
  };

  if (bridge === undefined || action === ACTIONS.load) return <SettingsSkeleton />;
  if (bridge === null) return <DesktopOnlyState />;

  const busy = action !== null;
  const candidateAvailable = apiKey.trim() !== '';

  return (
    <section className="studio-panel max-w-3xl p-5 sm:p-6" aria-labelledby="xai-settings-title">
      <div className="flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex items-center gap-2">
            <KeyRound className="size-5 text-primary" aria-hidden />
            <h2 id="xai-settings-title" className="font-[family-name:var(--font-display)] text-xl font-semibold text-foreground">
              Subtítulos con Grok
            </h2>
          </div>
          <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground">
            Usa tu propia clave de xAI para generar subtítulos. Se cifra con la protección de Windows y nunca se guarda en el navegador ni pasa por Next.js.
          </p>
        </div>
        <StatusBadge status={status} />
      </div>

      <div className="mt-5 grid gap-3 sm:grid-cols-2">
        <StatusRow label="Activa ahora" value={status ? SOURCE_LABELS[status.activeSource] : 'Comprobando…'} />
        <StatusRow label="Tras reiniciar" value={status ? SOURCE_LABELS[status.pendingSource] : 'Comprobando…'} />
      </div>

      {status?.storageError ? (
        <Message tone="error">{status.storageError}</Message>
      ) : null}

      <div className="mt-6 space-y-2">
        <Label htmlFor="xai-api-key">Clave API de xAI</Label>
        <Input
          id="xai-api-key"
          name="xai-api-key-new"
          type="password"
          autoComplete="new-password"
          spellCheck={false}
          maxLength={MAX_XAI_API_KEY_LENGTH}
          value={apiKey}
          onChange={(event) => setAPIKey(event.target.value)}
          placeholder={status?.stored ? 'Introduce una clave nueva para sustituir la guardada' : 'xai-…'}
          disabled={busy || status?.storageAvailable === false}
          aria-describedby="xai-key-help"
        />
        <p id="xai-key-help" className="text-xs leading-5 text-muted-foreground">
          FragForge nunca vuelve a mostrar una clave guardada. Probar comprueba la clave escrita; Guardar la cifra para este usuario de Windows.
        </p>
      </div>

      {testResult ? (
        <Message tone={testResult.ok ? 'success' : 'error'}>
          {testResult.message}
        </Message>
      ) : null}
      {error ? <Message tone="error">{error}</Message> : null}

      <div className="mt-6 flex flex-col gap-3 sm:flex-row sm:flex-wrap">
        <Button type="button" variant="outline" onClick={() => void runTest()} disabled={busy || !candidateAvailable}>
          {action === ACTIONS.test ? <LoaderCircle className="size-4 animate-spin" aria-hidden /> : <RefreshCw className="size-4" aria-hidden />}
          Probar conexión
        </Button>
        <Button
          type="button"
          onClick={() => void save()}
          disabled={busy || !candidateAvailable || status?.storageAvailable === false}
        >
          {action === ACTIONS.save ? <LoaderCircle className="size-4 animate-spin" aria-hidden /> : <KeyRound className="size-4" aria-hidden />}
          Guardar clave
        </Button>
        <Button
          type="button"
          variant="destructive"
          onClick={() => void remove()}
          disabled={busy || !status?.stored}
        >
          {action === ACTIONS.remove ? <LoaderCircle className="size-4 animate-spin" aria-hidden /> : <Trash2 className="size-4" aria-hidden />}
          Eliminar clave
        </Button>
      </div>

      <div className={cn(
        'mt-6 flex flex-col gap-4 border p-4 sm:flex-row sm:items-center sm:justify-between',
        status?.restartRequired ? 'border-warning/45 bg-warning/5' : 'border-border bg-muted/20',
      )}>
        <div className="flex gap-3">
          <TriangleAlert className={cn('mt-0.5 size-5 shrink-0', status?.restartRequired ? 'text-warning' : 'text-muted-foreground')} aria-hidden />
          <div>
            <p className="text-sm font-medium text-foreground">
              {status?.restartRequired ? 'Reinicio pendiente' : 'No hay cambios pendientes'}
            </p>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              La clave solo se aplica al reiniciar. Reiniciar corta cualquier grabación o render que esté en curso.
            </p>
          </div>
        </div>
        <Button
          type="button"
          variant="secondary"
          className="shrink-0"
          onClick={() => void restart()}
          disabled={busy || !status?.restartRequired}
        >
          {action === ACTIONS.restart ? <LoaderCircle className="size-4 animate-spin" aria-hidden /> : <RefreshCw className="size-4" aria-hidden />}
          Reiniciar ahora
        </Button>
      </div>
    </section>
  );
}

function StatusBadge({ status }: { status: XAISettingsStatus | null }): ReactNode {
  if (status === null) {
    return <span className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-muted-foreground">Comprobando</span>;
  }
  if (status.restartRequired) {
    return (
      <span className="inline-flex items-center gap-1.5 bg-warning/10 px-2 py-1 font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-warning">
        <TriangleAlert className="size-3.5" aria-hidden /> Pendiente
      </span>
    );
  }
  if (status.active) {
    return (
      <span className="inline-flex items-center gap-1.5 bg-success/10 px-2 py-1 font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-success">
        <CircleCheck className="size-3.5" aria-hidden /> Activa
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 bg-muted px-2 py-1 font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-muted-foreground">
      <WifiOff className="size-3.5" aria-hidden /> Sin configurar
    </span>
  );
}

function StatusRow({ label, value }: { label: string; value: string }): ReactNode {
  return (
    <div className="border border-border bg-muted/20 px-4 py-3">
      <p className="font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.14em] text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm font-medium text-foreground">{value}</p>
    </div>
  );
}

function Message({ tone, children }: { tone: 'success' | 'error'; children: ReactNode }): ReactNode {
  return (
    <p
      role={tone === 'error' ? 'alert' : 'status'}
      className={cn(
        'mt-4 border px-4 py-3 text-sm leading-5',
        tone === 'success' ? 'border-success/40 bg-success/5 text-success' : 'border-destructive/40 bg-destructive/5 text-destructive',
      )}
    >
      {children}
    </p>
  );
}

function DesktopOnlyState(): ReactNode {
  return (
    <DesktopOnlyCard titleId="desktop-only-title" title="Credenciales de subtítulos con Grok">
      Abre esta pantalla desde la aplicación de escritorio para guardar la clave con la protección de Windows. Por seguridad no existe una alternativa HTTP en el navegador.
    </DesktopOnlyCard>
  );
}

function SettingsSkeleton(): ReactNode {
  return (
    <section className="studio-panel max-w-3xl p-6" aria-label="Cargando ajustes de xAI">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <LoaderCircle className="size-5 animate-spin text-primary" aria-hidden />
        Leyendo la configuración protegida…
      </div>
    </section>
  );
}

function redactTestResult(result: XAIConnectionTestResult, candidate: string): XAIConnectionTestResult {
  if (candidate === '' || !result.message.includes(candidate)) return result;
  return { ...result, message: result.message.replaceAll(candidate, '[clave oculta]') };
}
