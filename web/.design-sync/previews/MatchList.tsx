import { MatchList } from 'cs2video-web';

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
        width: 780,
      }}
    >
      {children}
    </div>
  );
}

const h = (hours: number) =>
  new Date(Date.now() - 1000 * 60 * 60 * hours).toISOString();

const matches = [
  {
    id: 'm1',
    map: 'Mirage',
    score: '16-12',
    playedAt: h(3),
    stats: { kills: 31, deaths: 14, assists: 7, mvps: 5, kd: 2.21 },
    decentPlays: 4,
  },
  {
    id: 'm2',
    map: 'Inferno',
    score: '11-16',
    playedAt: h(26),
    stats: { kills: 18, deaths: 20, assists: 5, mvps: 2, kd: 0.9 },
    decentPlays: 1,
  },
  {
    id: 'm3',
    map: 'Nuke',
    score: '16-9',
    playedAt: h(52),
    stats: { kills: 27, deaths: 16, assists: 9, mvps: 4, kd: 1.69 },
    decentPlays: 3,
  },
];

export function Scoreboard() {
  return (
    <Frame>
      <MatchList matches={matches} />
    </Frame>
  );
}

export function Empty() {
  return (
    <Frame>
      <MatchList matches={[]} />
    </Frame>
  );
}
