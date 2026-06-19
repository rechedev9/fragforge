import { MatchRow } from 'cs2video-web';

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
        width: 760,
      }}
    >
      {children}
    </div>
  );
}

const won = {
  id: 'm1',
  map: 'Mirage',
  score: '16-12',
  playedAt: new Date(Date.now() - 1000 * 60 * 60 * 3).toISOString(),
  stats: { kills: 31, deaths: 14, assists: 7, mvps: 5, kd: 2.21 },
  decentPlays: 4,
};

const lost = {
  id: 'm2',
  map: 'Inferno',
  score: '11-16',
  playedAt: new Date(Date.now() - 1000 * 60 * 60 * 26).toISOString(),
  stats: { kills: 18, deaths: 20, assists: 5, mvps: 2, kd: 0.9 },
  decentPlays: 1,
};

export function Won() {
  return (
    <Frame>
      <MatchRow match={won} />
    </Frame>
  );
}

export function Lost() {
  return (
    <Frame>
      <MatchRow match={lost} />
    </Frame>
  );
}
