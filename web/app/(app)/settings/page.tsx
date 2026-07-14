import type { ReactNode } from 'react';
import { Settings } from 'lucide-react';
import { StudioPageHeader } from '@/components/studio/page-header';
import { XAISettings } from '@/components/settings/xai-settings';

/** Desktop-only application settings. Secret handling remains in Electron. */
export default function SettingsPage(): ReactNode {
  return (
    <div className="flex flex-col gap-8">
      <StudioPageHeader
        number={6}
        label="AJUSTES"
        title="Configuración"
        description="Configura las credenciales opcionales que FragForge usa para generar subtítulos en tu propio equipo."
        actions={<Settings className="hidden size-8 text-primary/70 sm:block" aria-hidden />}
      />
      <XAISettings />
    </div>
  );
}
