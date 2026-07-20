import type { ReactNode } from 'react';
import { StudioPageHeader } from '@/components/studio/page-header';
import { XAISettings } from '@/components/settings/xai-settings';
import { StudioInfo } from '@/components/settings/studio-info';
import { navSection } from '@/lib/nav';

const NAV = navSection('/settings');

/** Desktop-only application settings. Secret handling remains in Electron. */
export default function SettingsPage(): ReactNode {
  return (
    <div className="flex flex-col gap-8">
      <StudioPageHeader
        number={Number(NAV.number)}
        label={NAV.label.toUpperCase()}
        title="CONFIGURACIÓN"
        description="Consulta la versión instalada y configura las credenciales opcionales de subtítulos. El agente integrado usa tu sesión personal de Codex."
      />
      <StudioInfo />
      <XAISettings />
    </div>
  );
}
