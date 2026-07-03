'use client';

import type { Play } from '@/lib/api/types';
import { Button } from '@/components/ui/button';
import { PlayRow } from './play-row';

export type PlayListProps = {
  /** Highlights in plan order; rows render in this order top to bottom. */
  plays: Play[];
  selectedIds: ReadonlySet<string>;
  onToggle: (id: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
};

/**
 * PlayList — the vertical, scroll-with-the-page successor to the horizontal
 * Filmstrip of PlayTiles. One bordered row per highlight (PlayRow), a compact
 * mono header with the selection count plus Seleccionar todo / Limpiar, and no
 * horizontal scroll at any width.
 */
export function PlayList({ plays, selectedIds, onToggle, onSelectAll, onClear }: PlayListProps) {
  const allSelected = plays.length > 0 && selectedIds.size === plays.length;

  return (
    <div className="flex flex-col overflow-hidden border border-primary/15">
      <div className="flex items-center justify-between gap-3 border-b border-primary/15 bg-muted/30 px-3 py-2">
        <span className="font-[family-name:var(--font-mono)] text-[10px] tracking-[0.14em] text-muted-foreground/70">
          {selectedIds.size > 0
            ? `${selectedIds.size} ${selectedIds.size === 1 ? 'SELECCIONADA' : 'SELECCIONADAS'}`
            : 'TOCA PARA SELECCIONAR'}
        </span>
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            disabled={allSelected}
            onClick={onSelectAll}
            className="font-[family-name:var(--font-mono)] text-[10px] tracking-[0.14em]"
          >
            SELECCIONAR TODO
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            disabled={selectedIds.size === 0}
            onClick={onClear}
            className="font-[family-name:var(--font-mono)] text-[10px] tracking-[0.14em]"
          >
            LIMPIAR
          </Button>
        </div>
      </div>

      {plays.map((play) => (
        <PlayRow key={play.id} play={play} selected={selectedIds.has(play.id)} onToggle={() => onToggle(play.id)} />
      ))}
    </div>
  );
}
