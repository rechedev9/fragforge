import { PresetCards } from 'cs2video-web';

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
        width: 540,
      }}
    >
      {children}
    </div>
  );
}

const presets = [
  {
    name: 'clean-pov-60',
    label: 'Clean POV',
    description: 'Minimal HUD, your raw aim front and center. Best for clip purists.',
    hudMode: 'Clean POV',
    default: true,
  },
  {
    name: 'viral-60-clean',
    label: 'Music edit',
    description: 'Beat-synced cuts, zoom punches, and a lime kill feed for the algorithm.',
    hudMode: 'Kill Feed',
  },
];

export function Picker() {
  return (
    <Frame>
      <PresetCards presets={presets} value="viral-60-clean" onChange={() => {}} />
    </Frame>
  );
}
