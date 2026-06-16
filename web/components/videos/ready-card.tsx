'use client';

import { useState } from 'react';
import { Clock, Download, Eye, Globe, Share2 } from 'lucide-react';
import { toast } from 'sonner';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { formatCountdown } from '@/lib/format';
import { Card } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ReelCover } from '@/components/brand';

/**
 * A finished, downloadable video. Shows the rendered reel's cover (falling back
 * to a generated ReelCover), a hover overlay (View / Download / Share), title,
 * map · score in mono, an availability countdown chip, and a Publish toggle.
 * View plays the reel inline in a dialog; the 9:16 short fits the portrait frame.
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
      toast('Link copied to clipboard.');
    } catch {
      toast('Could not copy the link.');
    }
  };

  const meta = video.score ? `${video.map} · ${video.score}` : video.map;

  return (
    <>
      <Card className="gap-0 overflow-hidden py-0">
        <div className="group relative aspect-video w-full bg-muted">
          {video.thumbnailUrl ? (
            // eslint-disable-next-line @next/next/no-img-element -- proxied reel cover, dynamic same-origin URL
            <img src={video.thumbnailUrl} alt="" className="size-full object-cover" />
          ) : (
            <ReelCover seed={video.id} label={video.map} className="size-full" />
          )}

          {video.published ? (
            <div className="absolute right-3 top-3">
              <Badge>
                <Globe /> Published
              </Badge>
            </div>
          ) : null}

          {/* hover overlay actions */}
          <div className="absolute inset-0 flex items-center justify-center gap-2 bg-background/70 opacity-0 backdrop-blur-[1px] transition-opacity duration-200 group-hover:opacity-100">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => video.downloadUrl && setPlayerOpen(true)}
              disabled={!video.downloadUrl}
            >
              <Eye /> View
            </Button>
            <Button variant="default" size="sm" onClick={handleDownload} disabled={!video.downloadUrl}>
              <Download /> Download
            </Button>
            <Button variant="ghost" size="sm" onClick={handleShare} disabled={!video.downloadUrl}>
              <Share2 /> Share
            </Button>
          </div>
        </div>

        <div className="flex flex-col gap-3 p-4">
          <div className="min-w-0">
            <p className="truncate font-semibold text-foreground">{video.title}</p>
            <p className="mt-0.5 font-[family-name:var(--font-mono)] text-sm tabular-nums text-muted-foreground">
              {meta}
            </p>
          </div>

          <div className="flex items-center justify-between gap-3">
            {video.availableForSec !== undefined ? (
              <span className="inline-flex items-center gap-1.5 rounded-full border border-border bg-secondary/40 px-2.5 py-1 text-xs text-muted-foreground">
                <Clock className="size-3.5" />
                <span className="font-[family-name:var(--font-mono)] tabular-nums">
                  expires in {formatCountdown(video.availableForSec)}
                </span>
              </span>
            ) : (
              <span />
            )}

            {video.published ? (
              <Badge>
                <Globe /> Published
              </Badge>
            ) : (
              <Button variant="outline" size="sm" onClick={publish} disabled={publishing}>
                <Globe /> {publishing ? 'Publishing…' : 'Publish'}
              </Button>
            )}
          </div>
        </div>
      </Card>

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
