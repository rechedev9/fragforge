'use client';

import { useEffect, useState, type ReactNode } from 'react';
import { CircleCheck, Copy, LoaderCircle, Plug, TriangleAlert } from 'lucide-react';
import { toast } from 'sonner';
import { getDesktopSettingsBridge, type MCPConfigInfo } from '@/lib/desktop-settings';
import { Button } from '@/components/ui/button';
import { DesktopOnlyCard } from '@/components/settings/desktop-only-card';

/**
 * Registration card for the local MCP server: shows this machine's launcher
 * and copies ready-to-use snippets for Claude Code (CLI command) and for
 * mcpServers-style clients (Claude Desktop, Codex). Read-only — the real
 * registration happens in the user's own agent client.
 */
export function MCPSettings(): ReactNode {
  const [config, setConfig] = useState<MCPConfigInfo | null>(null);
  const [state, setState] = useState<'loading' | 'browser' | 'error' | 'ready'>('loading');

  useEffect(() => {
    const bridge = getDesktopSettingsBridge();
    if (bridge === null) {
      setState('browser');
      return;
    }
    let mounted = true;
    bridge.getMCPConfig()
      .then((info) => {
        if (!mounted) return;
        setConfig(info);
        setState('ready');
      })
      .catch(() => {
        if (mounted) setState('error');
      });
    return () => {
      mounted = false;
    };
  }, []);

  if (state === 'browser') {
    return (
      <DesktopOnlyCard titleId="mcp-desktop-only-title" title="Registro del asistente por MCP">
        Abre esta pantalla desde la aplicación de escritorio: el comando de registro incluye la ruta real del lanzador MCP instalado en tu equipo.
      </DesktopOnlyCard>
    );
  }

  return (
    <section className="studio-panel max-w-3xl p-6" aria-labelledby="mcp-settings-title">
      <div className="flex items-start justify-between gap-4">
        <div className="flex gap-4">
          <Plug className="mt-0.5 size-6 shrink-0 text-primary" aria-hidden />
          <div>
            <h2 id="mcp-settings-title" className="font-[family-name:var(--font-display)] text-xl font-semibold text-foreground">
              Servidor MCP local
            </h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              Conecta FragForge a Claude Code, Claude Desktop o Codex como servidor MCP. El lanzador
              detecta el Studio en ejecución automáticamente, sin configurar puertos ni variables.
            </p>
          </div>
        </div>
        <LauncherBadge config={config} state={state} />
      </div>

      {state === 'loading' ? (
        <div className="mt-6 flex items-center gap-3 text-sm text-muted-foreground">
          <LoaderCircle className="size-5 animate-spin text-primary" aria-hidden />
          Localizando el lanzador MCP…
        </div>
      ) : null}

      {state === 'error' ? (
        <p role="alert" className="mt-4 border border-destructive/40 bg-destructive/5 px-4 py-3 text-sm leading-5 text-destructive">
          No se pudo leer la configuración del MCP.
        </p>
      ) : null}

      {state === 'ready' && config !== null ? (
        <div className="mt-6 flex flex-col gap-4">
          <div className="border border-border bg-muted/20 px-4 py-3">
            <p className="font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.14em] text-muted-foreground">Lanzador</p>
            <p className="mt-1 break-all font-[family-name:var(--font-mono)] text-xs leading-5 text-foreground">{config.launcherPath}</p>
          </div>
          <ConfigSnippet
            title="Claude Code"
            description="Ejecuta este comando una vez en tu terminal."
            snippet={config.claudeCommand}
            copyLabel="Copiar comando de Claude Code"
          />
          <ConfigSnippet
            title="Claude Desktop / Codex (mcpServers)"
            description="Añade este bloque al fichero de configuración MCP de tu cliente."
            snippet={config.mcpServersJSON}
            copyLabel="Copiar JSON de mcpServers"
          />
          <p className="text-xs leading-5 text-muted-foreground">
            FragForge Studio debe estar abierto cuando el asistente use las herramientas <code>search</code> y <code>execute</code>.
          </p>
        </div>
      ) : null}
    </section>
  );
}

function LauncherBadge({ config, state }: { config: MCPConfigInfo | null; state: string }): ReactNode {
  if (state !== 'ready' || config === null) {
    return <span className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-muted-foreground">Comprobando</span>;
  }
  if (config.launcherInstalled) {
    return (
      <span className="inline-flex items-center gap-1.5 bg-success/10 px-2 py-1 font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-success">
        <CircleCheck className="size-3.5" aria-hidden /> Instalado
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 bg-warning/10 px-2 py-1 font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-warning">
      <TriangleAlert className="size-3.5" aria-hidden /> No encontrado
    </span>
  );
}

function ConfigSnippet({
  title,
  description,
  snippet,
  copyLabel,
}: {
  title: string;
  description: string;
  snippet: string;
  copyLabel: string;
}): ReactNode {
  const copy = async (): Promise<void> => {
    try {
      await navigator.clipboard.writeText(snippet);
      toast.success(`${title}: configuración copiada.`);
    } catch {
      toast.error('No se pudo copiar al portapapeles.');
    }
  };

  return (
    <div className="border border-border bg-muted/20 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-sm font-medium text-foreground">{title}</p>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{description}</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => void copy()} aria-label={copyLabel}>
          <Copy className="size-4" aria-hidden />
          Copiar
        </Button>
      </div>
      <pre className="mt-3 overflow-x-auto border border-border/60 bg-background/60 p-3 font-[family-name:var(--font-mono)] text-[11px] leading-5 text-foreground">
        {snippet}
      </pre>
    </div>
  );
}
