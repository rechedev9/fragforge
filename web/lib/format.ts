import type { VideoStatus } from './api/types';

// Re-export the canonical `cn` (clsx + tailwind-merge) so legacy
// `@/lib/format` imports keep resolving after the v2 shadcn migration.
export { cn } from './utils';

export function formatKd(n: number): string {
  return n.toFixed(2);
}

/** Tailwind text-colour class for an HLTV-1.0 rating, by performance band. */
export function ratingClass(rating: number): string {
  if (rating >= 1.15) return 'text-emerald-400';
  if (rating >= 0.95) return 'text-foreground';
  if (rating >= 0.8) return 'text-amber-400';
  return 'text-rose-400';
}

/** Relative time like "2h" / "3d" / "just now" from an ISO string or epoch ms. */
export function timeAgo(value: string | number): string {
  const then = typeof value === 'number' ? value : Date.parse(value);
  const diffSec = Math.max(0, (Date.now() - then) / 1000);

  if (diffSec < 60) return 'just now';
  const minutes = Math.floor(diffSec / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

/** Remaining-availability countdown: "14h" or "13h 59m" or "12m". */
export function formatCountdown(sec: number): string {
  const total = Math.max(0, Math.floor(sec));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);

  if (hours <= 0) return `${minutes}m`;
  if (minutes === 0) return `${hours}h`;
  return `${hours}h ${minutes}m`;
}

/**
 * Product-facing label for a render status. The pipeline's internal stages are
 * collapsed into the three words users see: Capturing, Processing, Ready.
 */
export function productStatusLabel(status: VideoStatus): string {
  switch (status) {
    case 'recording':
      return 'Capturing';
    case 'queued':
    case 'composing':
      return 'Processing';
    case 'ready':
      return 'Ready';
    case 'failed':
      return 'Failed';
  }
}
