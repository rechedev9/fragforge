import { Toggle } from 'cs2video-web';
import { Volume2, Captions, Star } from 'lucide-react';

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
        gap: 12,
        alignItems: 'center',
        flexWrap: 'wrap',
      }}
    >
      {children}
    </div>
  );
}

export function States() {
  return (
    <Frame>
      <Toggle pressed aria-label="Captions on">
        <Captions /> Captions
      </Toggle>
      <Toggle aria-label="Game audio off">
        <Volume2 /> Game audio
      </Toggle>
      <Toggle pressed aria-label="Favorite">
        <Star /> Best frags
      </Toggle>
    </Frame>
  );
}

export function OutlineVariant() {
  return (
    <Frame>
      <Toggle variant="outline" pressed aria-label="Slow-mo on">
        Slow-mo
      </Toggle>
      <Toggle variant="outline" aria-label="Kill feed off">
        Kill feed
      </Toggle>
      <Toggle variant="outline" size="sm" pressed aria-label="Watermark on">
        Watermark
      </Toggle>
    </Frame>
  );
}
