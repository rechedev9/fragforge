import { SongPickerDialog } from 'cs2video-web';

// Controlled-open; the dialog fetches the mock song catalog (api shim) and
// renders the track rows. cardMode:"single" (config) hosts the portal'd dialog.
function Stage({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
      }}
    >
      {children}
    </div>
  );
}

export function PickATrack() {
  return (
    <Stage>
      <SongPickerDialog
        open
        onOpenChange={() => {}}
        onChoose={() => {}}
        selectedSongId={null}
      />
    </Stage>
  );
}
