'use client';

import { useState } from 'react';
import { Twitch } from 'lucide-react';
import type { NormalizedRect, StreamVariant } from '@/lib/api/streams';
import {
  calculateCropCoverGeometry,
  representativeFrameTime,
  type FrameSize,
} from '@/lib/stream-preview';

const FULL_FRAME: NormalizedRect = { x: 0, y: 0, width: 1, height: 1 };

const PREVIEW_LAYOUTS: Record<
  StreamVariant,
  { face?: FrameSize; gameplay: FrameSize }
> = {
  'streamer-vertical-stack-40-60': {
    face: { width: 1080, height: 768 },
    gameplay: { width: 1080, height: 1152 },
  },
  'streamer-vertical-stack': {
    face: { width: 1080, height: 520 },
    gameplay: { width: 1080, height: 1400 },
  },
  'streamer-fullframe-nocam': {
    gameplay: { width: 1080, height: 1920 },
  },
};

/**
 * Renders one output band with the same geometry as FFmpeg: crop the source
 * rect, scale it proportionally until it covers the band, then center-crop the
 * excess. The video element itself always keeps the source aspect ratio.
 */
function CroppedFrame({
  videoSrc,
  rect,
  output,
  band,
  className,
}: {
  videoSrc: string;
  rect: NormalizedRect;
  output: FrameSize;
  band: 'facecam' | 'gameplay';
  className?: string;
}) {
  const [source, setSource] = useState<FrameSize | null>(null);
  const geometry = source ? calculateCropCoverGeometry(rect, source, output) : null;

  return (
    <div className={className} style={{ overflow: 'hidden', position: 'relative' }} data-preview-band={band}>
      <video
        src={videoSrc}
        muted
        playsInline
        preload="auto"
        aria-hidden="true"
        data-stream-frame={`preview-${band}`}
        onLoadedMetadata={(event) => {
          const video = event.currentTarget;
          if (video.videoWidth > 0 && video.videoHeight > 0) {
            setSource({ width: video.videoWidth, height: video.videoHeight });
          }
          video.currentTime = representativeFrameTime(video.duration);
        }}
        onSeeked={(event) => event.currentTarget.pause()}
        style={{
          position: 'absolute',
          width: geometry ? `${geometry.widthPercent}%` : '100%',
          height: geometry ? `${geometry.heightPercent}%` : '100%',
          left: geometry ? `${geometry.leftPercent}%` : '0',
          top: geometry ? `${geometry.topPercent}%` : '0',
          maxWidth: 'none',
        }}
      />
    </div>
  );
}

/**
 * Live 9:16 preview: facecam over gameplay for stack variants, or gameplay
 * only for the no-facecam variant. Band sizes and crop geometry mirror the
 * render variant registry in internal/streamclips.
 */
export function StreamPreview({
  videoSrc,
  variant,
  faceCrop,
  gameplayCrop,
  streamerNick,
}: {
  videoSrc: string;
  variant: StreamVariant;
  faceCrop?: NormalizedRect;
  gameplayCrop?: NormalizedRect;
  streamerNick?: string;
}) {
  const gameplay = gameplayCrop ?? FULL_FRAME;
  const layout = PREVIEW_LAYOUTS[variant];
  const facePct = layout.face
    ? (layout.face.height * 100) / (layout.face.height + layout.gameplay.height)
    : 0;
  const bannerCenterPct = layout.face ? facePct : 20;

  return (
    <div className="relative mx-auto aspect-[9/16] w-full max-w-[220px] overflow-hidden rounded-xl border border-border bg-black shadow-lg">
      <div className="flex h-full w-full flex-col">
        {layout.face ? (
          <div style={{ height: `${facePct}%` }} className="w-full">
            <CroppedFrame
              videoSrc={videoSrc}
              rect={faceCrop ?? FULL_FRAME}
              output={layout.face}
              band="facecam"
              className="h-full w-full"
            />
          </div>
        ) : null}
        <div style={{ height: layout.face ? `${100 - facePct}%` : '100%' }} className="w-full">
          <CroppedFrame
            videoSrc={videoSrc}
            rect={gameplay}
            output={layout.gameplay}
            band="gameplay"
            className="h-full w-full"
          />
        </div>
      </div>
      {streamerNick ? (
        <div
          className="absolute left-0 flex h-[5%] w-full -translate-y-1/2 items-center bg-[#9146ff] text-white shadow-sm"
          style={{ top: `${bannerCenterPct}%` }}
        >
          <span className="flex h-full w-[11%] shrink-0 items-center justify-center bg-[#5b1ba9]">
            <Twitch className="h-[62%] w-[62%]" strokeWidth={2.6} aria-hidden />
          </span>
          <span className="truncate px-[3%] font-[family-name:var(--font-display)] text-[clamp(7px,3.2vw,12px)] font-black leading-none tracking-[0.02em]">
            {streamerNick}
          </span>
        </div>
      ) : null}
    </div>
  );
}
