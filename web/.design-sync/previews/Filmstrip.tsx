import { Filmstrip, ReelCover, RecDot } from 'cs2video-web';
import { Play } from 'lucide-react';

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
        padding: 28,
        borderRadius: 14,
        border: '1px solid var(--border)',
        width: 560,
      }}
    >
      {children}
    </div>
  );
}

const tiles = [
  { id: 'p1', map: 'Mirage', label: '4K mid', kills: 4, selected: true },
  { id: 'p2', map: 'Inferno', label: 'Banana ace', kills: 5, selected: false },
  { id: 'p3', map: 'Nuke', label: 'Ramp hold', kills: 3, selected: false },
  { id: 'p4', map: 'Ancient', label: 'A retake', kills: 4, selected: false },
  { id: 'p5', map: 'Dust2', label: 'Long pick', kills: 2, selected: false },
];

function Tile({
  map,
  label,
  kills,
  selected,
}: {
  map: string;
  label: string;
  kills: number;
  selected: boolean;
}) {
  return (
    <div
      style={{
        position: 'relative',
        width: 140,
        height: 84,
        borderRadius: 10,
        overflow: 'hidden',
        border: selected ? '2px solid var(--primary)' : '1px solid var(--border)',
        cursor: 'pointer',
      }}
    >
      <ReelCover seed={`${map}-${label}`} label={map} />
      <span
        style={{
          position: 'absolute',
          inset: 0,
          display: 'grid',
          placeItems: 'center',
          color: 'rgba(255,255,255,0.9)',
        }}
      >
        <Play style={{ width: 22, height: 22, fill: 'currentColor' }} />
      </span>
      <span
        style={{
          position: 'absolute',
          top: 6,
          right: 6,
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
          fontWeight: 600,
          color: 'var(--primary)',
        }}
      >
        {kills}K
      </span>
      <span
        style={{
          position: 'absolute',
          bottom: 6,
          left: 8,
          fontSize: 11,
          fontWeight: 600,
          color: 'rgba(255,255,255,0.85)',
        }}
      >
        {label}
      </span>
    </div>
  );
}

export function PlayTiles() {
  return (
    <Frame>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 12,
        }}
      >
        <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--foreground)' }}>
          Forge highlights
        </span>
        <RecDot label="LIVE ON YOUR RIG" />
      </div>
      <Filmstrip>
        {tiles.map((t) => (
          <Tile key={t.id} {...t} />
        ))}
      </Filmstrip>
    </Frame>
  );
}
