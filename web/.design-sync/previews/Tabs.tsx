import { Tabs, TabsList, TabsTrigger, TabsContent } from 'cs2video-web';
import { Crosshair, Music, Film } from 'lucide-react';

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
        width: 440,
      }}
    >
      {children}
    </div>
  );
}

export function RenderModes() {
  return (
    <Frame>
      <Tabs defaultValue="clean">
        <TabsList>
          <TabsTrigger value="clean">
            <Crosshair /> Clean POV
          </TabsTrigger>
          <TabsTrigger value="music">
            <Music /> Music edit
          </TabsTrigger>
        </TabsList>
        <TabsContent value="clean">
          <div style={{ padding: '14px 2px', fontSize: 13, lineHeight: 1.6 }}>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>Mirage · 4-piece</div>
            <div style={{ color: 'var(--muted-foreground)' }}>
              Raw demo POV, HUD on. Exported at 1080×1920, 60fps.
            </div>
          </div>
        </TabsContent>
        <TabsContent value="music">
          <div style={{ padding: '14px 2px', fontSize: 13, lineHeight: 1.6 }}>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>Beat-synced cut</div>
            <div style={{ color: 'var(--muted-foreground)' }}>
              Kills aligned to drops, HUD hidden, grain overlay applied.
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </Frame>
  );
}

export function ClipScope() {
  return (
    <Frame>
      <Tabs defaultValue="highlights">
        <TabsList>
          <TabsTrigger value="highlights">Highlights</TabsTrigger>
          <TabsTrigger value="rounds">Rounds</TabsTrigger>
          <TabsTrigger value="clutches">
            <Film /> Clutches
          </TabsTrigger>
        </TabsList>
        <TabsContent value="highlights">
          <div style={{ padding: '14px 2px', fontSize: 13, color: 'var(--muted-foreground)' }}>
            12 decent plays detected across the match.
          </div>
        </TabsContent>
        <TabsContent value="rounds">
          <div style={{ padding: '14px 2px', fontSize: 13, color: 'var(--muted-foreground)' }}>
            Browse every round and pick segments to clip.
          </div>
        </TabsContent>
        <TabsContent value="clutches">
          <div style={{ padding: '14px 2px', fontSize: 13, color: 'var(--muted-foreground)' }}>
            2 clutches won this match (1v3, 1v2).
          </div>
        </TabsContent>
      </Tabs>
    </Frame>
  );
}
