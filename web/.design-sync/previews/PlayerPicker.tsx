import { PlayerPicker } from 'cs2video-web';

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
        width: 460,
        display: 'flex',
        flexDirection: 'column',
        gap: 14,
      }}
    >
      {children}
    </div>
  );
}

// Roster arrives sorted by kills desc; the clip-worthiest player (by multi-kill
// rounds) is auto-highlighted and tagged "Recommended".
const roster = [
  { steamId: '76561198000000001', name: 's1mple', team: 'CT' as const, kills: 31, deaths: 16, assists: 5, rounds5k: 1, rounds4k: 0, rounds3k: 2 },
  { steamId: '76561198000000002', name: 'ZywOo', team: 'CT' as const, kills: 27, deaths: 18, assists: 7, rounds3k: 1 },
  { steamId: '76561198000000003', name: 'NiKo', team: 'T' as const, kills: 24, deaths: 19, assists: 4 },
  { steamId: '76561198000000004', name: 'donk', team: 'T' as const, kills: 22, deaths: 20, assists: 9, rounds4k: 1 },
  { steamId: '76561198000000005', name: 'm0NESY', team: 'CT' as const, kills: 19, deaths: 21, assists: 3 },
  { steamId: '76561198000000006', name: 'sh1ro', team: 'T' as const, kills: 14, deaths: 23, assists: 6 },
];

const match = { map: 'de_dust2', scoreCt: 9, scoreT: 13, rounds: 22 };

export function Roster() {
  return (
    <Frame>
      <span
        style={{
          fontFamily: 'var(--font-display)',
          fontSize: 18,
          fontWeight: 600,
          letterSpacing: '-0.01em',
        }}
      >
        Who do you want to clip?
      </span>
      <PlayerPicker players={roster} onPick={() => {}} match={match} />
    </Frame>
  );
}
