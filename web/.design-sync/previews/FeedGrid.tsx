import { FeedGrid } from 'cs2video-web';

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
        width: 620,
      }}
    >
      {children}
    </div>
  );
}

// Dark 9:16 inline-SVG thumbnail — images don't load offline, this renders in
// the headless capture. Hue/angle vary per card for visual variety.
function thumb(angle: number, glow: string) {
  const svg = `<svg xmlns='http://www.w3.org/2000/svg' width='270' height='480' viewBox='0 0 270 480'>
    <defs>
      <linearGradient id='g' x1='0' y1='0' x2='1' y2='1' gradientTransform='rotate(${angle} .5 .5)'>
        <stop offset='0' stop-color='%23262a2e'/>
        <stop offset='1' stop-color='%2314161a'/>
      </linearGradient>
      <radialGradient id='l' cx='0.8' cy='0.15' r='0.55'>
        <stop offset='0' stop-color='${glow}' stop-opacity='0.2'/>
        <stop offset='1' stop-color='${glow}' stop-opacity='0'/>
      </radialGradient>
    </defs>
    <rect width='270' height='480' fill='url(%23g)'/>
    <rect width='270' height='480' fill='url(%23l)'/>
  </svg>`;
  return `data:image/svg+xml,${svg.replace(/\n\s*/g, '')}`;
}

const h = (hours: number) => Date.now() - 1000 * 60 * 60 * hours;

const items = [
  {
    id: 'f1',
    author: 'shroud_btw',
    authorAvatarUrl: '',
    title: '4K AWP wallbang to close the half',
    map: 'Mirage',
    thumbnailUrl: thumb(15, '%23c4f000'),
    likes: 1284,
    createdAt: h(2),
    videoUrl: '',
  },
  {
    id: 'f2',
    author: 'banana_king',
    authorAvatarUrl: '',
    title: 'Banana ace, full save denied',
    map: 'Inferno',
    thumbnailUrl: thumb(55, '%23ff8a3d'),
    likes: 932,
    createdAt: h(7),
    videoUrl: '',
  },
  {
    id: 'f3',
    author: 'rampgod',
    authorAvatarUrl: '',
    title: 'Ramp hold 1v3 clutch on Nuke',
    map: 'Nuke',
    thumbnailUrl: thumb(95, '%234da3ff'),
    likes: 2471,
    createdAt: h(20),
    videoUrl: '',
  },
  {
    id: 'f4',
    author: 'mid_diff',
    authorAvatarUrl: '',
    title: 'A-site retake with the deag',
    map: 'Ancient',
    thumbnailUrl: thumb(130, '%23b07cff'),
    likes: 558,
    createdAt: h(31),
    videoUrl: '',
  },
  {
    id: 'f5',
    author: 'long_doubts',
    authorAvatarUrl: '',
    title: 'Long pick into the rotate read',
    map: 'Dust2',
    thumbnailUrl: thumb(40, '%2300d0a0'),
    likes: 1790,
    createdAt: h(50),
    videoUrl: '',
  },
  {
    id: 'f6',
    author: 'one_tap_andy',
    authorAvatarUrl: '',
    title: 'Eco round triple, all headshots',
    map: 'Mirage',
    thumbnailUrl: thumb(70, '%23c4f000'),
    likes: 411,
    createdAt: h(73),
    videoUrl: '',
  },
];

export function Masonry() {
  return (
    <Frame>
      <FeedGrid items={items} />
    </Frame>
  );
}
