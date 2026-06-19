import { Label, Input } from 'cs2video-web';
import { Flame } from 'lucide-react';

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
        width: 340,
      }}
    >
      {children}
    </div>
  );
}

export function FormField() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <Label htmlFor="reel-title">Reel title</Label>
        <Input id="reel-title" defaultValue="Mirage ace — 1v4 retake" />
        <span style={{ fontSize: 12, color: 'var(--muted-foreground)' }}>
          Shown as the YouTube Short headline.
        </span>
      </div>
    </Frame>
  );
}

export function WithIconAndDisabled() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Label htmlFor="hl-min">
            <Flame style={{ width: 14, height: 14, color: 'var(--primary)' }} />
            Min highlight rating
          </Label>
          <Input id="hl-min" type="number" defaultValue={4} style={{ fontFamily: 'var(--font-mono)' }} />
        </div>
        <div className="group" data-disabled="true" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Label htmlFor="hltv">HLTV demo (coming soon)</Label>
          <Input id="hltv" placeholder="Pro demos not yet supported" disabled />
        </div>
      </div>
    </Frame>
  );
}
