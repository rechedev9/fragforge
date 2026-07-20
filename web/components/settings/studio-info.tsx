'use client';

import { useEffect, useState } from 'react';
import { MonitorCog } from 'lucide-react';
import { getDesktopSettingsBridge, type StudioAppInfo } from '@/lib/desktop-settings';

export function StudioInfo() {
  const [info, setInfo] = useState<StudioAppInfo | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    const bridge = getDesktopSettingsBridge();
    if (!bridge) {
      setUnavailable(true);
      return;
    }
    void bridge.getAppInfo().then(setInfo).catch(() => setUnavailable(true));
  }, []);
  return (
    <section className="studio-panel flex flex-col gap-4 p-5 sm:p-6" aria-labelledby="studio-info-title">
      <div className="flex items-center gap-3">
        <MonitorCog className="size-5 text-primary" />
        <h2 id="studio-info-title" className="font-[family-name:var(--font-display)] text-lg font-bold">FRAGFORGE STUDIO</h2>
      </div>
      {info ? (
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          <div><dt className="text-muted-foreground">Versión instalada</dt><dd className="font-[family-name:var(--font-mono)]">{info.version}</dd></div>
          <div><dt className="text-muted-foreground">Build</dt><dd className="font-[family-name:var(--font-mono)]">{info.build}</dd></div>
          <div><dt className="text-muted-foreground">Electron</dt><dd className="font-[family-name:var(--font-mono)]">{info.electronVersion}</dd></div>
          <div><dt className="text-muted-foreground">Chromium</dt><dd className="font-[family-name:var(--font-mono)]">{info.chromiumVersion}</dd></div>
        </dl>
      ) : (
        <p className="text-sm text-muted-foreground">{unavailable ? 'La versión instalada solo está disponible dentro de la app de escritorio.' : 'Leyendo versión instalada…'}</p>
      )}
    </section>
  );
}
