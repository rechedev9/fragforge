import type { ReactNode } from 'react';
import { NewsShortWorkspace } from '@/components/news/news-short-workspace';
import { StudioPageHeader } from '@/components/studio/page-header';

export default function NewsPage(): ReactNode {
  return (
    <div className="flex flex-col gap-8">
      <StudioPageHeader
        number={4}
        label="NOTICIAS"
        title="SHORTS DE ACTUALIDAD"
        description="Convierte anuncios, publicaciones y reacciones de la comunidad en un proyecto vertical con fuente visible, enfoque crítico y tu propia voz local."
      />
      <NewsShortWorkspace />
    </div>
  );
}
