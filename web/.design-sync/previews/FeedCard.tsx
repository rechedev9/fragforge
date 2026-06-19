import { FeedCard } from 'cs2video-web';

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
        width: 280,
      }}
    >
      {children}
    </div>
  );
}

// A dark 9:16 inline-SVG thumbnail (images don't load offline; this renders in
// the headless capture). Two charcoal stops with a faint lime corner glow.
function thumb(angle: number) {
  const svg = `<svg xmlns='http://www.w3.org/2000/svg' width='270' height='480' viewBox='0 0 270 480'>
    <defs>
      <linearGradient id='g' x1='0' y1='0' x2='1' y2='1' gradientTransform='rotate(${angle} .5 .5)'>
        <stop offset='0' stop-color='%23222428'/>
        <stop offset='1' stop-color='%2315171a'/>
      </linearGradient>
      <radialGradient id='l' cx='0.85' cy='0.12' r='0.5'>
        <stop offset='0' stop-color='%23c4f000' stop-opacity='0.18'/>
        <stop offset='1' stop-color='%23c4f000' stop-opacity='0'/>
      </radialGradient>
    </defs>
    <rect width='270' height='480' fill='url(%23g)'/>
    <rect width='270' height='480' fill='url(%23l)'/>
  </svg>`;
  return `data:image/svg+xml,${svg.replace(/\n\s*/g, '')}`;
}

const item = {
  id: 'f1',
  author: 'shroud_btw',
  authorAvatarUrl: '',
  title: '4K AWP wallbang to close the half on Mirage',
  map: 'Mirage',
  thumbnailUrl: thumb(20),
  likes: 1284,
  createdAt: Date.now() - 1000 * 60 * 60 * 5,
  videoUrl: '',
};

export function Reel() {
  return (
    <Frame>
      <FeedCard item={item} />
    </Frame>
  );
}
