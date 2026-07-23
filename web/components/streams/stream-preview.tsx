'use client';

import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Twitch } from 'lucide-react';
import { streamsApi, type KillfeedKill, type NormalizedRect, type StreamClipRange, type StreamVariant } from '@/lib/api/streams';
import { DEFAULT_OVERLAY_FONT_SIZE } from '@/lib/clip-edit';
import { StreamFrameCanvas, useStreamFrame } from '@/components/streams/stream-frame-session';
import {
  activeTextOverlays,
  clampStreamerBannerPosition,
  killfeedBaseTopPixels,
  killfeedKillsForCue,
  killfeedNoticePlacement,
  killfeedSampleFrameSeconds,
  proportionalEvenKillfeedHeight,
  resolveActiveKillfeedCue,
  resolveStreamerBannerPosition,
  STREAMER_BANNER_MAX_POSITION,
  STREAMER_BANNER_MIN_POSITION,
  type FrameSize,
} from '@/lib/stream-preview';

const FULL_FRAME: NormalizedRect = { x: 0, y: 0, width: 1, height: 1 };
const EMPTY_CLIPS: StreamClipRange[] = [];
const PREVIEW_WIDTH = 1080;
const PREVIEW_HEIGHT = 1920;
const KILLFEED_WIDTH = 930;

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
  rect,
  output,
  band,
  className,
}: {
  rect: NormalizedRect;
  output: FrameSize;
  band: 'facecam' | 'gameplay';
  className?: string;
}) {
  return (
    <div className={className} style={{ overflow: 'hidden', position: 'relative' }} data-preview-band={band}>
      <StreamFrameCanvas
        mode="cover"
        rect={rect}
        outputWidth={output.width}
        outputHeight={output.height}
        className="absolute inset-0 h-full w-full"
      />
    </div>
  );
}

/**
 * Shows the exact selected source crop. Unlike the gameplay and facecam bands,
 * this does not use cover geometry: the whole normalized notice rectangle is
 * scaled to the backend's fixed 930-pixel width and proportional even height.
 */
function KillfeedOverlayFrame({
  rect,
  sampleSeconds,
  topPixels,
  visible,
}: {
  rect: NormalizedRect;
  sampleSeconds: number;
  topPixels: number;
  visible: boolean;
}) {
  const frame = useStreamFrame();
  const requestSnapshot = frame.requestSnapshot;
  const [bitmap, setBitmap] = useState<ImageBitmap | null>(null);
  const bitmapRef = useRef<ImageBitmap | null>(null);
  const source = frame.sourceWidth > 0 && frame.sourceHeight > 0
    ? { width: frame.sourceWidth, height: frame.sourceHeight }
    : null;
  const outputHeight = source ? proportionalEvenKillfeedHeight(rect, source) : null;

  useEffect(() => {
    if (!visible || outputHeight === null) {
      setBitmap((current) => {
        current?.close();
        bitmapRef.current = null;
        return null;
      });
      return;
    }
    let cancelled = false;
    void requestSnapshot(sampleSeconds, rect, KILLFEED_WIDTH, outputHeight).then((next) => {
      if (cancelled) {
        next?.close();
        return;
      }
      setBitmap((current) => {
        current?.close();
        bitmapRef.current = next;
        return next;
      });
    });
    return () => {
      cancelled = true;
    };
  }, [outputHeight, rect, requestSnapshot, sampleSeconds, visible]);

  useEffect(() => () => bitmapRef.current?.close(), []);

  return (
    <div
      aria-hidden="true"
      data-preview-killfeed
      data-killfeed-visible={visible}
      className={`pointer-events-none absolute overflow-hidden ${visible && outputHeight !== null ? '' : 'invisible'}`}
      style={{
        width: `${(KILLFEED_WIDTH * 100) / PREVIEW_WIDTH}%`,
        height: outputHeight === null ? '0' : `${(outputHeight * 100) / PREVIEW_HEIGHT}%`,
        left: `${(((PREVIEW_WIDTH - KILLFEED_WIDTH) / 2) * 100) / PREVIEW_WIDTH}%`,
        top: `${(topPixels * 100) / PREVIEW_HEIGHT}%`,
      }}
    >
      {bitmap ? <FrozenFrameCanvas bitmap={bitmap} /> : null}
    </div>
  );
}

function FrozenFrameCanvas({ bitmap }: { bitmap: ImageBitmap }): ReactNode {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    canvas.getContext('2d', { alpha: false })?.drawImage(bitmap, 0, 0);
  }, [bitmap]);
  return <canvas ref={canvasRef} aria-hidden className="absolute inset-0 h-full w-full" />;
}

/**
 * Loads the synthetic notice PNG for each kill through the notice-preview proxy
 * and returns a ready object URL per kill (null until its image is ready).
 * Images are cached and deduped by JSON.stringify(kill); every object URL is
 * revoked when the preview unmounts.
 */
function useKillfeedNoticeUrls(kills: KillfeedKill[]): (string | null)[] {
  const cacheRef = useRef<Map<string, string>>(new Map());
  const pendingRef = useRef<Set<string>>(new Set());
  const [ready, setReady] = useState<Record<string, string>>({});
  const killsKey = JSON.stringify(kills);

  useEffect(() => {
    let cancelled = false;
    const cache = cacheRef.current;
    const pending = pendingRef.current;
    for (const kill of kills) {
      const key = JSON.stringify(kill);
      if (cache.has(key) || pending.has(key)) continue;
      pending.add(key);
      streamsApi
        .previewKillfeedNotice(kill)
        .then((blob) => {
          if (cancelled) return;
          const url = URL.createObjectURL(blob);
          cache.set(key, url);
          setReady((prev) => ({ ...prev, [key]: url }));
        })
        .catch(() => {
          // Leave the notice hidden until a later attempt succeeds.
        })
        .finally(() => {
          pending.delete(key);
        });
    }
    return () => {
      cancelled = true;
    };
    // killsKey captures the kill payloads; `ready` is intentionally not a dep.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [killsKey]);

  useEffect(() => {
    const cache = cacheRef.current;
    return () => {
      for (const url of cache.values()) URL.revokeObjectURL(url);
      cache.clear();
    };
  }, []);

  return kills.map((kill) => ready[JSON.stringify(kill)] ?? null);
}

/**
 * Horizontally centered, upward-growing stack of synthetic kill notices for a
 * cue that has confirmed kills. Geometry mirrors the render (72px notices, 8px
 * gap) scaled to the preview box; a notice is shown only once its image is
 * ready. This is static placement parity only: the render also adds a slide-in
 * and fade entrance/exit the preview intentionally omits.
 */
function SyntheticKillfeedNotices({
  kills,
  baseTopPixels,
}: {
  kills: KillfeedKill[];
  baseTopPixels: number;
}) {
  const urls = useKillfeedNoticeUrls(kills);

  return (
    <div aria-hidden="true" data-preview-killfeed-notices className="pointer-events-none absolute inset-0">
      {kills.map((kill, index) => {
        const url = urls[index];
        if (!url) return null;
        const placement = killfeedNoticePlacement(index, baseTopPixels);
        return (
          <img
            // eslint-disable-next-line @next/next/no-img-element
            key={`${index}-${JSON.stringify(kill)}`}
            src={url}
            alt=""
            data-preview-killfeed-notice
            className="absolute"
            style={{
              top: `${placement.topPercent}%`,
              left: '50%',
              transform: 'translateX(-50%)',
              height: `${placement.heightPercent}%`,
              width: 'auto',
              maxWidth: 'none',
            }}
          />
        );
      })}
    </div>
  );
}

/**
 * Live 9:16 preview: facecam over gameplay for stack variants, or gameplay
 * only for the no-facecam variant. Band sizes and crop geometry mirror the
 * render variant registry in internal/streamclips.
 */
export function StreamPreview({
  variant,
  faceCrop,
  gameplayCrop,
  killfeedCrop,
  clips = EMPTY_CLIPS,
  frameSeconds,
  streamerNick,
  streamerPositionY,
  streamerSlideEnabled = false,
  onStreamerPositionChange,
  disabled = false,
}: {
  variant: StreamVariant;
  faceCrop?: NormalizedRect;
  gameplayCrop?: NormalizedRect;
  killfeedCrop?: NormalizedRect;
  clips?: StreamClipRange[];
  frameSeconds: number;
  streamerNick?: string;
  streamerPositionY?: number;
  streamerSlideEnabled?: boolean;
  onStreamerPositionChange?: (position: number) => void;
  disabled?: boolean;
}): ReactNode {
  const containerRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ startClientY: number; startPosition: number } | null>(null);
  const gameplay = gameplayCrop ?? FULL_FRAME;
  const layout = PREVIEW_LAYOUTS[variant];
  const faceLayout = layout.face;
  const facePct = faceLayout
    ? (faceLayout.height * 100) / (faceLayout.height + layout.gameplay.height)
    : 0;
  const bannerPosition = resolveStreamerBannerPosition(variant, streamerPositionY);
  const killfeedTop = killfeedBaseTopPixels(faceLayout ? faceLayout.height : 0, layout.gameplay.height);
  const activeKillfeedCue = killfeedCrop
    ? resolveActiveKillfeedCue(clips, frameSeconds)
    : null;
  const activeKills = activeKillfeedCue !== null ? killfeedKillsForCue(clips, activeKillfeedCue) : [];
  const activeOverlays = activeTextOverlays(clips, frameSeconds);

  const beginBannerDrag = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (disabled || !onStreamerPositionChange) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = { startClientY: event.clientY, startPosition: bannerPosition };
  }, [bannerPosition, disabled, onStreamerPositionChange]);

  const moveBanner = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    const container = containerRef.current;
    if (!drag || !container || !onStreamerPositionChange) return;
    const height = container.clientHeight;
    if (height <= 0) return;
    onStreamerPositionChange(clampStreamerBannerPosition(drag.startPosition + (event.clientY - drag.startClientY) / height));
  }, [onStreamerPositionChange]);

  const endBannerDrag = useCallback(() => {
    dragRef.current = null;
  }, []);

  const moveBannerWithKeyboard = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    if (disabled || !onStreamerPositionChange) return;
    let next: number | undefined;
    if (event.key === 'ArrowUp') next = bannerPosition - 0.01;
    if (event.key === 'ArrowDown') next = bannerPosition + 0.01;
    if (event.key === 'Home') next = STREAMER_BANNER_MIN_POSITION;
    if (event.key === 'End') next = STREAMER_BANNER_MAX_POSITION;
    if (next === undefined) return;
    event.preventDefault();
    onStreamerPositionChange(clampStreamerBannerPosition(next));
  }, [bannerPosition, disabled, onStreamerPositionChange]);

  return (
    <div
      ref={containerRef}
      className="relative mx-auto aspect-[9/16] w-full max-w-[220px] overflow-hidden rounded-xl border border-border bg-background shadow-lg"
      style={{ containerType: 'size' }}
    >
      <div className="flex h-full w-full flex-col">
        {faceLayout ? (
          <div style={{ height: `${facePct}%` }} className="w-full">
            <CroppedFrame
              rect={faceCrop ?? FULL_FRAME}
              output={faceLayout}
              band="facecam"
              className="h-full w-full"
            />
          </div>
        ) : null}
        <div style={{ height: faceLayout ? `${100 - facePct}%` : '100%' }} className="w-full">
          <CroppedFrame
            rect={gameplay}
            output={layout.gameplay}
            band="gameplay"
            className="h-full w-full"
          />
        </div>
      </div>
      {killfeedCrop && activeKills.length > 0 ? (
        <SyntheticKillfeedNotices kills={activeKills} baseTopPixels={killfeedTop} />
      ) : null}
      {killfeedCrop && activeKills.length === 0 ? (
        <KillfeedOverlayFrame
          rect={killfeedCrop}
          sampleSeconds={activeKillfeedCue === null ? frameSeconds : killfeedSampleFrameSeconds(clips, activeKillfeedCue)}
          topPixels={killfeedTop}
          visible={activeKillfeedCue !== null}
        />
      ) : null}
      {activeOverlays.map((overlay, i) => (
        <span
          key={i}
          className="pointer-events-none absolute left-0 w-full -translate-y-1/2 px-[4%] text-center font-[family-name:var(--font-display)] font-black leading-tight text-white"
          style={{
            top: `${overlay.position_y * 100}%`,
            // Match the render: font_size output pixels on the 1920px-tall canvas.
            fontSize: `${((overlay.font_size ?? DEFAULT_OVERLAY_FONT_SIZE) / PREVIEW_HEIGHT) * 100}cqh`,
            textShadow: '0 0 2px rgba(0,0,0,0.9), 0 1px 2px rgba(0,0,0,0.55)',
          }}
        >
          {overlay.text}
        </span>
      ))}
      {streamerNick ? (
        <div
          role="slider"
          tabIndex={disabled ? -1 : 0}
          aria-label="Posición del banner en la vista previa"
          aria-orientation="vertical"
          aria-valuemin={STREAMER_BANNER_MIN_POSITION * 100}
          aria-valuemax={STREAMER_BANNER_MAX_POSITION * 100}
          aria-valuenow={Math.round(bannerPosition * 1000) / 10}
          aria-disabled={disabled}
          data-streamer-banner
          onPointerDown={beginBannerDrag}
          onPointerMove={moveBanner}
          onPointerUp={endBannerDrag}
          onPointerCancel={endBannerDrag}
          onKeyDown={moveBannerWithKeyboard}
          className={`absolute left-0 h-[5%] w-full -translate-y-1/2 touch-none select-none ${disabled ? 'cursor-default opacity-60' : 'cursor-ns-resize'}`}
          style={{ top: `${bannerPosition * 100}%` }}
        >
          <div className={`flex h-full w-full items-center bg-[#9146ff] text-white shadow-sm ${streamerSlideEnabled ? 'streamer-banner-slide-preview' : ''}`}>
            <span className="flex h-full w-[11%] shrink-0 items-center justify-center bg-[#5b1ba9]">
              <Twitch className="h-[62%] w-[62%]" strokeWidth={2.6} aria-hidden />
            </span>
            <span className="truncate px-[3%] font-[family-name:var(--font-display)] text-[clamp(7px,3.2vw,12px)] font-black leading-none tracking-[0.02em]">
              {streamerNick}
            </span>
          </div>
        </div>
      ) : null}
      <style>{`
        .streamer-banner-slide-preview {
          animation: streamer-banner-slide-preview 2.8s ease-in-out 1;
        }

        @keyframes streamer-banner-slide-preview {
          0%, 10% { transform: translateX(-105%); }
          24%, 76% { transform: translateX(0); }
          90%, 100% { transform: translateX(-105%); }
        }

        @media (prefers-reduced-motion: reduce) {
          .streamer-banner-slide-preview { animation: none; }
        }
      `}</style>
    </div>
  );
}
