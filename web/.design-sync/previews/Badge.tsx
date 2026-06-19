import { Badge } from 'cs2video-web';
import { Globe, Film, Flame } from 'lucide-react';

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
        flexWrap: 'wrap',
        gap: 10,
        alignItems: 'center',
      }}
    >
      {children}
    </div>
  );
}

export function Variants() {
  return (
    <Frame>
      <Badge>Published</Badge>
      <Badge variant="secondary">Queued</Badge>
      <Badge variant="outline">Map</Badge>
      <Badge variant="destructive">Failed</Badge>
    </Frame>
  );
}

export function WithIcons() {
  return (
    <Frame>
      <Badge>
        <Globe /> Published
      </Badge>
      <Badge variant="secondary">
        <Film /> 4 highlights
      </Badge>
      <Badge variant="outline">
        <Flame /> Best frags
      </Badge>
    </Frame>
  );
}
