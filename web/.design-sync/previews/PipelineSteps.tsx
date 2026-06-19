import { PipelineSteps } from 'cs2video-web';

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
        gap: 22,
        width: 460,
      }}
    >
      {children}
    </div>
  );
}

function Row({ label, status }: { label: string; status: string }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <span
        style={{
          fontSize: 11,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          color: 'var(--muted-foreground)',
        }}
      >
        {label}
      </span>
      <PipelineSteps status={status as never} />
    </div>
  );
}

export function AllStages() {
  return (
    <Frame>
      <Row label="Queued" status="queued" />
      <Row label="Capturing on your rig" status="recording" />
      <Row label="Editing" status="composing" />
      <Row label="Ready to post" status="ready" />
      <Row label="Failed" status="failed" />
    </Frame>
  );
}
