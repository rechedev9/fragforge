import { PlayTile } from 'cs2video-web';

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
        padding: 24,
        borderRadius: 14,
        border: '1px solid var(--border)',
        display: 'flex',
        gap: 16,
        width: 500,
      }}
    >
      {children}
    </div>
  );
}

const selectedPlay = {
  id: 'play-mirage-r14',
  matchId: 'm1',
  label: '4K mid retake',
  kind: 'highlight' as const,
  round: 14,
  kills: 4,
  weapon: 'AK-47',
};

const unselectedPlay = {
  id: 'play-inferno-r07',
  matchId: 'm2',
  label: 'Banana ace',
  kind: 'highlight' as const,
  round: 7,
  kills: 5,
  weapon: 'M4A1-S',
};

export function Tiles() {
  return (
    <Frame>
      <PlayTile play={selectedPlay} selected onSelect={() => {}} />
      <PlayTile play={unselectedPlay} selected={false} onSelect={() => {}} />
    </Frame>
  );
}
