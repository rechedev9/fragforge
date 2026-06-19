import { Toaster } from 'cs2video-web';
import { CircleCheckIcon, Loader2Icon } from 'lucide-react';

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
        gap: 12,
        width: 380,
      }}
    >
      {children}
    </div>
  );
}

/**
 * Static representation of a sonner toast using the same popover tokens the
 * live Toaster applies (--popover / --popover-foreground / --border). The live
 * sonner region renders empty in static capture because no toast is queued and
 * nothing can dispatch one without an interaction.
 */
function ToastCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div
      style={{
        display: 'flex',
        gap: 10,
        alignItems: 'flex-start',
        background: 'var(--popover)',
        color: 'var(--popover-foreground)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius)',
        padding: '12px 14px',
        boxShadow: '0 4px 12px rgba(0,0,0,0.35)',
        fontSize: 13,
      }}
    >
      <span style={{ marginTop: 1, color: 'var(--muted-foreground)' }}>{icon}</span>
      <div style={{ lineHeight: 1.4 }}>
        <div style={{ fontWeight: 600 }}>{title}</div>
        <div style={{ color: 'var(--muted-foreground)', marginTop: 2 }}>{description}</div>
      </div>
    </div>
  );
}

export function Toasts() {
  return (
    <Frame>
      <Toaster />
      <ToastCard
        icon={<CircleCheckIcon className="size-4" />}
        title="Reel ready"
        description="Mirage · 4-piece is rendered and ready to download."
      />
      <ToastCard
        icon={<Loader2Icon className="size-4 animate-spin" />}
        title="Composing music edit"
        description="Beat-syncing kills to the selected track…"
      />
    </Frame>
  );
}
