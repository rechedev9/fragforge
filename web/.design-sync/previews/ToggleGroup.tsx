import { ToggleGroup, ToggleGroupItem } from 'cs2video-web';
import { Trophy, Flame, Clapperboard } from 'lucide-react';

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
        display: 'flex',
        flexDirection: 'column',
        gap: 18,
        alignItems: 'flex-start',
      }}
    >
      {children}
    </div>
  );
}

export function MatchFilter() {
  return (
    <Frame>
      <span style={{ fontSize: 11, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--muted-foreground)' }}>
        Show matches
      </span>
      <ToggleGroup type="single" defaultValue="wins" variant="outline">
        <ToggleGroupItem value="all">All</ToggleGroupItem>
        <ToggleGroupItem value="wins">Wins</ToggleGroupItem>
        <ToggleGroupItem value="frags">Best frags</ToggleGroupItem>
      </ToggleGroup>
    </Frame>
  );
}

export function ClipTypes() {
  return (
    <Frame>
      <span style={{ fontSize: 11, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--muted-foreground)' }}>
        Include clip types
      </span>
      <ToggleGroup type="multiple" defaultValue={['aces', 'clutch']} variant="outline">
        <ToggleGroupItem value="aces">
          <Trophy /> Aces
        </ToggleGroupItem>
        <ToggleGroupItem value="clutch">
          <Flame /> Clutches
        </ToggleGroupItem>
        <ToggleGroupItem value="multi">
          <Clapperboard /> Multi-kills
        </ToggleGroupItem>
      </ToggleGroup>
    </Frame>
  );
}
