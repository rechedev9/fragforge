'use client';

import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';

type SteamButtonProps = {
  onClick: () => void;
  loading?: boolean;
};

/** Steam's logo cog mark, hand-rolled so we don't pull in an extra asset. */
function SteamMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
      <path d="M11.98 2C6.73 2 2.42 6.03 2.03 11.18l5.36 2.22a2.83 2.83 0 0 1 1.6-.5l2.38-3.46v-.05a3.78 3.78 0 1 1 3.78 3.78h-.09l-3.4 2.43c0 .04.01.08.01.12a2.84 2.84 0 0 1-5.66.31L1.9 14.4A10 10 0 1 0 11.98 2Zm-3.5 15.18-1.23-.51a2.13 2.13 0 0 0 3.94-1.6 2.13 2.13 0 0 0-2.93-1.3l1.27.53a1.57 1.57 0 1 1-1.2 2.9l.15-.02Zm9.27-7.4a2.52 2.52 0 1 0-5.04 0 2.52 2.52 0 0 0 5.04 0Zm-4.41-.01a1.9 1.9 0 1 1 3.8 0 1.9 1.9 0 0 1-3.8 0Z" />
    </svg>
  );
}

/**
 * The lime "Continue with Steam" CTA — the one saturated element on the auth
 * hero. Everything else stays charcoal so this reads as the single action.
 */
export function SteamButton({ onClick, loading = false }: SteamButtonProps) {
  return (
    <Button
      type="button"
      size="lg"
      onClick={onClick}
      disabled={loading}
      className="h-12 gap-2.5 px-6 text-base font-semibold"
    >
      {loading ? (
        <Loader2 className="size-5 animate-spin" />
      ) : (
        <SteamMark className="size-5" />
      )}
      <span>{loading ? 'Signing in…' : 'Continue with Steam'}</span>
    </Button>
  );
}
