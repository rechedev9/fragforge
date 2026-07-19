import type { ReactNode } from 'react';
import { StudioPageHeader } from '@/components/studio/page-header';
import { MCPSettings } from '@/components/settings/mcp-settings';
import { XAISettings } from '@/components/settings/xai-settings';
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
        description="Configura las credenciales opcionales de subtítulos y conecta tu asistente por MCP, todo en tu propio equipo."
      />
      <XAISettings />
      <MCPSettings />
    </div>
  );
}
