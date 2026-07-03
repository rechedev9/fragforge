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

const STATUS_META: Record<CaptureStatus, { label: string; dot: string; text: string; hint: string }> = {
  ready: { label: 'Ready', dot: 'bg-primary', text: 'text-primary', hint: 'HLAE + CS2 detected on this PC.' },
  warning: { label: 'Check paths', dot: 'bg-amber-400', text: 'text-amber-400', hint: 'A capture tool was set but is missing on disk.' },
  unconfigured: { label: 'Set up', dot: 'bg-destructive', text: 'text-destructive', hint: "HLAE + CS2 weren't found on this PC." },
  offline: { label: 'Offline', dot: 'bg-muted-foreground', text: 'text-muted-foreground', hint: 'Start your local orchestrator (zv serve).' },
};

/** The three record tools, with a friendly name and a typical Windows path. */
const TOOL_GUIDE: Array<{ name: string; label: string; example: string }> = [
  { name: 'ZV_RECORDER_PATH', label: 'FragForge recorder', example: 'C:\\...\\bin\\zv-recorder.exe' },
  { name: 'ZV_HLAE_PATH', label: 'HLAE', example: 'C:\\HLAE-2.190.1\\HLAE.exe' },
  { name: 'ZV_CS2_PATH', label: 'CS2', example: 'C:\\Program Files (x86)\\Steam\\steamapps\\common\\Counter-Strike Global Offensive\\game\\bin\\win64\\cs2.exe' },
];

/**
 * CaptureReadiness — a persistent sidebar card (mirroring SlotsMeter) that always
 * tells the user whether gameplay capture is set up on their machine, reading the
 * local orchestrator's /api/capabilities. Clicking it opens a dialog with the exact
 * env vars + example paths to set, each tool's live accessible state, and the
 * restart note. This is the always-on reminder so capture never silently fails.
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
          className="w-full rounded-lg border border-sidebar-border bg-sidebar-accent/40 p-2.5 text-left transition-colors hover:bg-sidebar-accent/70 group-data-[collapsible=icon]:hidden"
        >
          <div className="flex items-center justify-between">
            <span className="text-[0.65rem] font-medium uppercase tracking-wide text-sidebar-foreground/70">Capture</span>
            <span className={cn('inline-flex items-center gap-1.5 text-xs font-medium', meta.text)}>
              <span className={cn('size-1.5 rounded-full', meta.dot)} />
              {meta.label}
            </span>
          </div>
          <p className="mt-1 text-[0.7rem] leading-snug text-sidebar-foreground/60">{meta.hint}</p>
        </button>
      </DialogTrigger>

      <DialogContent>
        <DialogHeader>
          <DialogTitle>Gameplay capture</DialogTitle>
          <DialogDescription>
            FragForge records on your PC with HLAE + CS2 and finds them automatically. Here is what it detected on this machine:
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          {TOOL_GUIDE.map((tool) => {
            const state = toolState.get(tool.name);
            const found = Boolean(state?.accessible);
            const badge = found
              ? { label: state?.source === 'env' ? 'Set' : 'Detected', cls: 'bg-primary/10 text-primary' }
              : state?.configured
                ? { label: 'Missing', cls: 'bg-amber-400/10 text-amber-400' }
                : { label: 'Not found', cls: 'bg-destructive/10 text-destructive' };
            return (
              <div key={tool.name} className="rounded-lg border border-border bg-card p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm font-medium text-foreground">{tool.label}</span>
                  <span className={cn('shrink-0 rounded-full px-1.5 py-0.5 font-[family-name:var(--font-mono)] text-[0.6rem] font-semibold uppercase tracking-wider', badge.cls)}>
                    {badge.label}
                  </span>
                </div>
                {found ? (
                  <p className="mt-1 text-xs text-muted-foreground">Ready to use.</p>
                ) : (
                  <>
                    <p className="mt-1 text-xs text-muted-foreground">
                      Not found. Install it, or set <code className="font-[family-name:var(--font-mono)]">{tool.name}</code> to its path.
                    </p>
                    <p className="mt-1 break-all font-[family-name:var(--font-mono)] text-[0.7rem] text-muted-foreground/70">typically {tool.example}</p>
                  </>
                )}
              </div>
            );
          })}
        </div>

        <p className="text-xs text-muted-foreground">
          Installed in the usual place? It is picked up automatically. To use a custom location, set the env var above and restart the
          orchestrator (re-run <code className="font-[family-name:var(--font-mono)]">zv serve</code>); worker config is read at startup.
        </p>

        <div className="flex justify-end">
          <Button variant="secondary" size="sm" onClick={() => void refresh()} disabled={loading}>
            <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
            Re-check
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
