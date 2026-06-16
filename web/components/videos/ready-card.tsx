'use client';

import { useState } from 'react';
import { Clock, Download, Eye, Globe, Share2 } from 'lucide-react';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { formatCountdown } from '@/lib/format';
import { Card } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ReelCover } from '@/components/brand';

/**
 * A finished, downloadable video. Thumbnail with a hover overlay (View /
 * Download / Share), title, map · score in mono, an availability countdown
 * chip, and a Publish toggle that flips to a lime "Published" badge.
 */
export function ReadyCard({ video, onChange }: { video: Video; onChange?: (v: Video) => void }) {
  const [publishing, setPublishing] = useState(false);

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

  const openExternal = (url?: string) => {
    if (url) window.open(url, '_blank', 'noopener,noreferrer');
  };

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="group relative aspect-video w-full bg-muted">
        <ReelCover seed={video.id} label={video.map} className="size-full" />

        {video.published ? (
          <div className="absolute right-3 top-3">
            <Badge>
              <Globe /> Published
            </Badge>
          </div>
        ) : null}

        {/* hover overlay actions */}
        <div className="absolute inset-0 flex items-center justify-center gap-2 bg-background/70 opacity-0 backdrop-blur-[1px] transition-opacity duration-200 group-hover:opacity-100">
          <Button variant="secondary" size="sm" onClick={() => openExternal(video.downloadUrl)}>
            <Eye /> View
          </Button>
          <Button variant="default" size="sm" onClick={() => openExternal(video.downloadUrl)}>
            <Download /> Download
          </Button>
          <Button variant="ghost" size="sm" onClick={() => openExternal(video.downloadUrl)}>
            <Share2 /> Share
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-3 p-4">
        <div className="min-w-0">
          <p className="truncate font-semibold text-foreground">{video.title}</p>
          <p className="mt-0.5 font-[family-name:var(--font-mono)] text-sm tabular-nums text-muted-foreground">
            {video.map} · {video.score}
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
  );
}
