'use client';

import { useState } from 'react';
import { Clock, Download, Eye, Globe, Share2 } from 'lucide-react';
import { toast } from 'sonner';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { formatCountdown } from '@/lib/format';
import { Badge } from '@/components/ui/badge';
import { PipelineSteps } from '@/components/brand/pipeline-steps';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ReelCover } from '@/components/brand/reel-cover';
import { DeleteVideoButton } from '@/components/videos/delete-video-button';

const FORMAT_LABEL: Record<string, string> = { 'short-9x16': '9:16', 'landscape-16x9': '16:9' };

/**
 * A finished, downloadable video — the mockup's LISTO card: a cyan corner
 * bracket, a "● LISTO" status tag, and a notched PUBLICAR CTA next to an
 * outlined MP4 download. Hovering the thumbnail still surfaces View/Share
 * (there is no thumbnail-duration data to show a real running time, so the
 * corner badge shows only the render format). View plays the reel inline in a
 * dialog; the 9:16 short fits the portrait frame.
 */
export function ReadyCard({ video, onChange }: { video: Video; onChange?: (v: Video) => void }) {
  const [publishing, setPublishing] = useState(false);
  const [playerOpen, setPlayerOpen] = useState(false);

  const publish = async () => {
    if (video.published || publishing) return;
    setPublishing(true);
    try {
      const updated = await api.publishVideo(video.id);
      onChange?.(updated);
    } finally {
      setPublishing(false);
    }
  };

  const handleDownload = () => {
    if (!video.downloadUrl) return;
    const a = document.createElement('a');
    a.href = video.downloadUrl;
    a.download = `${video.title}.mp4`;
    a.rel = 'noopener';
    document.body.appendChild(a);
    a.click();
    a.remove();
  };

  // In cloud mode the reel's media is a DOM object URL (blob:) fetched through the
  // Bearer-gated loopback: it lives and dies with this tab, so there is no
  // persistent URL to share. Hide Share entirely there rather than copy a link
  // that dies with the tab. Download and inline playback still work with blob:.
  const canShare = video.downloadUrl != null && !video.downloadUrl.startsWith('blob:');

  const handleShare = async () => {
    if (!video.downloadUrl) return;
    const url = new URL(video.downloadUrl, window.location.origin).href;
    try {
      if (typeof navigator !== 'undefined' && navigator.share) {
        await navigator.share({ title: video.title, url });
        return;
      }
    } catch {
      // user dismissed the share sheet, or it failed — fall through to copy.
    }
    try {
      await navigator.clipboard.writeText(url);
      toast('Enlace copiado al portapapeles.');
    } catch {
      toast('No se pudo copiar el enlace.');
    }
  };

  const meta = video.score ? `${video.map} · ${video.score}` : video.map;
  const formatBadge = video.editConfig ? FORMAT_LABEL[video.editConfig.format] : undefined;

  return (
    <>
      <article
        data-slot="card"
        className="studio-panel studio-panel-raised studio-panel-interactive neon-brackets flex h-full flex-col"
      >
        <div className="group relative aspect-video w-full overflow-hidden border-b border-border bg-muted">
          {video.thumbnailUrl ? (
            // eslint-disable-next-line @next/next/no-img-element -- proxied reel cover, dynamic same-origin URL
            <img src={video.thumbnailUrl} alt="" className="size-full object-cover" />
          ) : (
            <ReelCover seed={video.id} label={video.map} className="size-full" />
          )}

          {formatBadge ? (
            <span className="absolute top-2.5 right-2.5 border border-primary/30 bg-background/90 px-2 py-1 font-[family-name:var(--font-mono)] text-[10px] tracking-[0.12em] text-primary">
              {formatBadge}
            </span>
          ) : null}

          {video.published ? (
            <div className="absolute top-2.5 left-2.5">
              <Badge className="border-success/35 bg-success/15 text-success">
                <Globe /> Publicado
              </Badge>
            </div>
          ) : null}

          {/* Hover on precise pointers; always visible and actionable on touch. */}
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center gap-2 bg-background/78 opacity-0 backdrop-blur-[1px] transition-opacity duration-200 group-focus-within:pointer-events-auto group-focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100 [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100">
            <button
              type="button"
              onClick={() => video.downloadUrl && setPlayerOpen(true)}
              disabled={!video.downloadUrl}
              className="inline-flex min-h-10 items-center gap-1.5 border border-primary/45 bg-background/85 px-4 text-xs font-semibold text-foreground outline-none transition-colors hover:bg-primary/15 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-40"
            >
              <Eye className="size-3.5" /> Ver
            </button>
            {canShare ? (
              <button
                type="button"
                onClick={handleShare}
                className="inline-flex min-h-10 items-center gap-1.5 border border-primary/45 bg-background/85 px-4 text-xs font-semibold text-foreground outline-none transition-colors hover:bg-primary/15 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
              >
                <Share2 className="size-3.5" /> Compartir
              </button>
            ) : null}
          </div>
        </div>

        <div className="flex flex-1 flex-col gap-4 p-4">
          <div className="min-w-0">
            <p className="truncate font-[family-name:var(--font-display)] text-base font-bold text-foreground">
              {video.title}
            </p>
            <p className="mt-1.5 truncate font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.1em] text-muted-foreground">
              {meta}
            </p>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="inline-flex min-h-7 items-center gap-1.5 border border-success/35 bg-success/10 px-2.5 font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.16em] text-success">
              <span className="size-1.5 rounded-full bg-success shadow-[0_0_7px_var(--success)]" />
              LISTO
            </span>
            {video.availableForSec !== undefined ? (
              <span className="inline-flex min-h-7 items-center gap-1.5 border border-warning/25 bg-warning/[0.06] px-2 font-[family-name:var(--font-mono)] text-[10px] text-warning">
                <Clock className="size-3.5" />
                caduca en {formatCountdown(video.availableForSec)}
              </span>
            ) : null}
          </div>

          <div className="border-y border-border/70 py-3">
            <PipelineSteps status={video.status} className="gap-x-2 text-[10px]" />
          </div>

          <div className="mt-auto grid grid-cols-[minmax(0,1fr)_minmax(0,0.78fr)_auto] items-center gap-2 border-t border-border/70 pt-4">
            {video.published ? (
              <span className="flex min-h-10 items-center justify-center gap-1.5 border border-success/35 bg-success/10 px-2 font-[family-name:var(--font-display)] text-xs font-bold tracking-[0.05em] text-success">
                <Globe className="size-3.5" /> PUBLICADO
              </span>
            ) : (
              <button
                type="button"
                onClick={publish}
                disabled={publishing}
                className="neon-notch min-h-10 bg-primary px-3 font-[family-name:var(--font-display)] text-xs font-bold tracking-[0.05em] text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
              >
                {publishing ? 'PUBLICANDO…' : 'PUBLICAR'}
              </button>
            )}
            <button
              type="button"
              onClick={handleDownload}
              disabled={!video.downloadUrl}
              className="flex min-h-10 items-center justify-center gap-1.5 border border-primary/40 px-3 font-[family-name:var(--font-display)] text-xs font-bold tracking-[0.05em] text-primary transition-colors hover:bg-primary/10 disabled:pointer-events-none disabled:opacity-40"
            >
              <Download className="size-3.5" /> MP4
            </button>
            <DeleteVideoButton video={video} onDeleted={() => onChange?.(video)} />
          </div>
        </div>
      </article>

      <Dialog open={playerOpen} onOpenChange={setPlayerOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="truncate">{video.title}</DialogTitle>
            <DialogDescription className="font-[family-name:var(--font-mono)] tabular-nums">
              {meta}
            </DialogDescription>
          </DialogHeader>
          {video.downloadUrl ? (
            <video
              src={video.downloadUrl}
              controls
              autoPlay
              playsInline
              className="mx-auto max-h-[72vh] w-auto rounded-lg bg-black"
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
