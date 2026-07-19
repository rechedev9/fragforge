import type { ReactNode } from 'react';
import { NewsShortWorkspace } from '@/components/news/news-short-workspace';
import { StudioPageHeader } from '@/components/studio/page-header';
import { navSection } from '@/lib/nav';

const NAV = navSection('/news');

export default function NewsPage(): ReactNode {
  return (
    <div className="flex flex-col gap-8">
      <StudioPageHeader
        number={Number(NAV.number)}
        label={NAV.label.toUpperCase()}
        title="SHORTS DE ACTUALIDAD"
        description="Convierte anuncios, publicaciones y reacciones de la comunidad en un proyecto vertical con fuente visible, enfoque crítico y tu propia voz local."
      />
      <NewsShortWorkspace />
    </div>
  );
}
