'use client';

import { useCallback, useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { api } from '@/lib/api';
import type { CaptureReadiness as CaptureReadinessData, CaptureStatus } from '@/lib/api/types';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

const STATUS_META: Record<CaptureStatus, { label: string; text: string; hint: string }> = {
  ready: { label: 'Lista', text: 'text-primary', hint: 'HLAE + CS2 detectado en este PC' },
  warning: { label: 'Revisa rutas', text: 'text-amber-400', hint: 'Una herramienta de captura está configurada pero falta en disco.' },
  unconfigured: { label: 'Configurar', text: 'text-destructive', hint: 'No se encontró HLAE + CS2 en este PC.' },
  offline: { label: 'Sin conexión', text: 'text-muted-foreground', hint: 'Arranca tu orquestador local (zv serve).' },
};

/** The three record tools, with a friendly name and a typical Windows path. */
const TOOL_GUIDE: Array<{ name: string; label: string; example: string }> = [
  { name: 'ZV_RECORDER_PATH', label: 'Grabador FragForge', example: 'C:\\...\\bin\\zv-recorder.exe' },
  { name: 'ZV_HLAE_PATH', label: 'HLAE', example: 'C:\\HLAE-2.190.1\\HLAE.exe' },
  { name: 'ZV_CS2_PATH', label: 'CS2', example: 'C:\\Program Files (x86)\\Steam\\steamapps\\common\\Counter-Strike Global Offensive\\game\\bin\\win64\\cs2.exe' },
];

/**
 * CaptureReadiness — the CAPTURA card pinned to the sidebar footer: a
 * bracket-cornered HUD panel that always tells the user whether gameplay
 * capture is set up on their machine, reading the local orchestrator's
 * /api/capabilities. Clicking it opens a dialog with the exact env vars +
 * example paths to set, each tool's live accessible state, and the restart
 * note. This is the always-on reminder so capture never silently fails.
 */
export function CaptureReadiness() {
  const [data, setData] = useState<CaptureReadinessData | null>(null);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setData(await api.getCaptureReadiness());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const status: CaptureStatus = data?.status ?? 'offline';
  const meta = STATUS_META[status];
  const toolState = new Map((data?.tools ?? []).map((t) => [t.name, t]));

  return (
    <Dialog>
      <DialogTrigger asChild>
        <button
          type="button"
          className="neon-brackets relative w-full border border-primary/25 bg-card/80 px-3.5 py-3 text-left transition-colors hover:bg-card group-data-[collapsible=icon]:hidden"
        >
          <div className="flex items-center justify-between font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.18em]">
            <span className="text-sidebar-foreground/60">Captura</span>
            <span className={cn('inline-flex items-center gap-1.5', meta.text)}>
              <span className="size-1.5 rounded-full bg-current shadow-[0_0_7px_currentColor]" />
              {meta.label}
            </span>
          </div>
          <p className="mt-1.5 text-[10.5px] leading-snug text-sidebar-foreground/60">{meta.hint}</p>
        </button>
      </DialogTrigger>

      <DialogContent>
        <DialogHeader>
          <DialogTitle>Captura de gameplay</DialogTitle>
          <DialogDescription>
            FragForge graba en tu PC con HLAE + CS2 y los encuentra automáticamente. Esto es lo que detectó en esta máquina:
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          {TOOL_GUIDE.map((tool) => {
            const state = toolState.get(tool.name);
            const found = Boolean(state?.accessible);
            let badge: { label: string; cls: string };
            if (found) {
              badge = { label: state?.source === 'env' ? 'Configurada' : 'Detectada', cls: 'bg-primary/10 text-primary' };
            } else if (state?.configured) {
              badge = { label: 'Falta', cls: 'bg-amber-400/10 text-amber-400' };
            } else {
              badge = { label: 'No encontrada', cls: 'bg-destructive/10 text-destructive' };
            }
            return (
              <div key={tool.name} className="border border-border bg-card p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm font-medium text-foreground">{tool.label}</span>
                  <span className={cn('shrink-0 px-1.5 py-0.5 font-[family-name:var(--font-mono)] text-[0.6rem] font-semibold uppercase tracking-wider', badge.cls)}>
                    {badge.label}
                  </span>
                </div>
                {found ? (
                  <p className="mt-1 text-xs text-muted-foreground">Lista para usar.</p>
                ) : (
                  <>
                    <p className="mt-1 text-xs text-muted-foreground">
                      No encontrada. Instálala, o apunta <code className="font-[family-name:var(--font-mono)]">{tool.name}</code> a su ruta.
                    </p>
                    <p className="mt-1 break-all font-[family-name:var(--font-mono)] text-[0.7rem] text-muted-foreground/70">normalmente {tool.example}</p>
                  </>
                )}
              </div>
            );
          })}
        </div>

        <p className="text-xs text-muted-foreground">
          ¿Instalada en el sitio habitual? Se detecta automáticamente. Para usar una ruta propia, define la variable de entorno de
          arriba y reinicia el orquestador (vuelve a lanzar <code className="font-[family-name:var(--font-mono)]">zv serve</code>); la
          configuración del worker se lee al arrancar.
        </p>

        <div className="flex justify-end">
          <Button variant="secondary" size="sm" onClick={() => void refresh()} disabled={loading}>
            <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
            Volver a comprobar
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
