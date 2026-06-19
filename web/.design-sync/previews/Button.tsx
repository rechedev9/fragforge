import { Button } from 'cs2video-web';
import { Play, Download, Plus, ArrowRight } from 'lucide-react';

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
        gap: 12,
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
      <Button>Create reel</Button>
      <Button variant="secondary">Secondary</Button>
      <Button variant="outline">Outline</Button>
      <Button variant="ghost">Ghost</Button>
      <Button variant="destructive">Delete</Button>
      <Button variant="link">Link</Button>
    </Frame>
  );
}

export function Sizes() {
  return (
    <Frame>
      <Button size="sm">Small</Button>
      <Button size="default">Default</Button>
      <Button size="lg">Large</Button>
      <Button size="icon" aria-label="Play">
        <Play />
      </Button>
    </Frame>
  );
}

export function WithIcons() {
  return (
    <Frame>
      <Button>
        <Play /> Forge highlights
      </Button>
      <Button variant="outline">
        <Download /> Download
      </Button>
      <Button variant="secondary">
        New reel <ArrowRight />
      </Button>
      <Button variant="ghost" size="icon" aria-label="Add">
        <Plus />
      </Button>
    </Frame>
  );
}

export function Disabled() {
  return (
    <Frame>
      <Button disabled>Disabled</Button>
      <Button variant="outline" disabled>
        Disabled
      </Button>
    </Frame>
  );
}
