'use client';

import { useState } from 'react';
import { Clock, Download, Eye, Globe, Share2 } from 'lucide-react';
import { toast } from 'sonner';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { formatCountdown } from '@/lib/format';
import { Badge } from '@/components/ui/badge';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ReelCover } from '@/components/brand';
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
      <div data-slot="card" className="neon-brackets relative border border-primary/40 bg-card/80">
        <div className="group relative aspect-video w-full overflow-hidden bg-muted">
          {video.thumbnailUrl ? (
            // eslint-disable-next-line @next/next/no-img-element -- proxied reel cover, dynamic same-origin URL
            <img src={video.thumbnailUrl} alt="" className="size-full object-cover" />
          ) : (
            <ReelCover seed={video.id} label={video.map} className="size-full" />
          )}

          {formatBadge ? (
            <span className="absolute top-2 right-2 bg-background/80 px-1.5 py-0.5 font-[family-name:var(--font-mono)] text-[10px] tracking-[0.12em] text-primary/80">
              {formatBadge}
            </span>
          ) : null}

          {video.published ? (
            <div className="absolute top-2 left-2">
              <Badge>
                <Globe /> Publicado
              </Badge>
            </div>
          ) : null}

          {/* hover overlay actions */}
          <div className="absolute inset-0 flex items-center justify-center gap-2 bg-background/70 opacity-0 backdrop-blur-[1px] transition-opacity duration-200 group-hover:opacity-100">
            <button
              type="button"
              onClick={() => video.downloadUrl && setPlayerOpen(true)}
              disabled={!video.downloadUrl}
              className="inline-flex items-center gap-1.5 border border-primary/40 bg-background/70 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-primary/10 disabled:pointer-events-none disabled:opacity-40"
            >
              <Eye className="size-3.5" /> Ver
            </button>
            <button
              type="button"
              onClick={handleShare}
              disabled={!video.downloadUrl}
              className="inline-flex items-center gap-1.5 border border-primary/40 bg-background/70 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-primary/10 disabled:pointer-events-none disabled:opacity-40"
            >
              <Share2 className="size-3.5" /> Compartir
            </button>
          </div>
        </div>

        <div className="flex flex-col gap-3 p-4">
          <div className="min-w-0">
            <p className="truncate font-[family-name:var(--font-display)] text-[14.5px] font-bold text-foreground">
              {video.title}
            </p>
            <p className="mt-1 flex items-center gap-1.5 font-[family-name:var(--font-mono)] text-[9.5px] uppercase tracking-[0.16em] text-primary">
              <span className="size-1.5 rounded-full bg-primary shadow-[0_0_7px_var(--primary)]" />
              LISTO
            </p>
          </div>

          {video.availableForSec !== undefined ? (
            <span className="inline-flex w-fit items-center gap-1.5 font-[family-name:var(--font-mono)] text-[10.5px] text-muted-foreground">
              <Clock className="size-3.5" />
              caduca en {formatCountdown(video.availableForSec)}
            </span>
          ) : null}

          <div className="flex items-center gap-2">
            {video.published ? (
              <span className="neon-notch neon-glow flex flex-1 items-center justify-center gap-1.5 bg-primary py-2 font-[family-name:var(--font-display)] text-xs font-bold tracking-[0.05em] text-primary-foreground">
                <Globe className="size-3.5" /> PUBLICADO
              </span>
            ) : (
              <button
                type="button"
                onClick={publish}
                disabled={publishing}
                className="neon-notch flex-1 bg-primary py-2 font-[family-name:var(--font-display)] text-xs font-bold tracking-[0.05em] text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
              >
                {publishing ? 'PUBLICANDO…' : 'PUBLICAR'}
              </button>
            )}
            <button
              type="button"
              onClick={handleDownload}
              disabled={!video.downloadUrl}
              className="flex flex-1 items-center justify-center gap-1.5 border border-primary/40 py-2 font-[family-name:var(--font-display)] text-xs font-bold tracking-[0.05em] text-primary/90 transition-colors hover:bg-primary/10 disabled:pointer-events-none disabled:opacity-40"
            >
              <Download className="size-3.5" /> MP4
            </button>
            <DeleteVideoButton video={video} onDeleted={() => onChange?.(video)} />
          </div>
        </div>
      </div>

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
