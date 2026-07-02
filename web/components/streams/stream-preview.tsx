import type { NormalizedRect, StreamVariant } from '@/lib/api/streams';

const FULL_FRAME: NormalizedRect = { x: 0, y: 0, width: 1, height: 1 };

/**
 * Renders the portion of `videoSrc` inside `rect` (normalized 0..1 of the
 * source frame) scaled to fill its container. This is the standard CSS
 * "zoomed background" trick: the video is sized to 1/rect.width x 1/rect.height
 * of its wrapper and offset by -rect.x/-rect.y at that scale, so the crop
 * region exactly fills the overflow-hidden wrapper regardless of the wrapper's
 * own pixel size. It is a design-time approximation only (the real crop
 * happens in the FFmpeg render), which is enough for a live preview.
 */
function CroppedFrame({ videoSrc, rect, className }: { videoSrc: string; rect: NormalizedRect; className?: string }) {
  const scaleX = rect.width > 0 ? 1 / rect.width : 1;
  const scaleY = rect.height > 0 ? 1 / rect.height : 1;
  return (
    <div className={className} style={{ overflow: 'hidden', position: 'relative' }}>
      <video
        src={videoSrc}
        muted
        playsInline
        preload="metadata"
        style={{
          position: 'absolute',
          width: `${scaleX * 100}%`,
          height: `${scaleY * 100}%`,
          left: `${-rect.x * scaleX * 100}%`,
          top: `${-rect.y * scaleY * 100}%`,
          objectFit: 'fill',
        }}
      />
    </div>
  );
}

/**
 * Live 9:16 preview mock: facecam on top (~40%) and gameplay below (~60%) for
 * the stack variants, or gameplay only for the no-facecam variant. Purely a
 * CSS approximation of the final render layout, driven by the same
 * face_crop/gameplay_crop the editor writes to the edit plan.
 */
export function StreamPreview({
  videoSrc,
  variant,
  faceCrop,
  gameplayCrop,
}: {
  videoSrc: string;
  variant: StreamVariant;
  faceCrop?: NormalizedRect;
  gameplayCrop?: NormalizedRect;
}) {
  const gameplay = gameplayCrop ?? FULL_FRAME;
  const showFace = variant !== 'streamer-fullframe-nocam';
  const facePct = variant === 'streamer-vertical-stack-40-60' ? 40 : 50;

  return (
    <div className="mx-auto aspect-[9/16] w-full max-w-[220px] overflow-hidden rounded-xl border border-border bg-black shadow-lg">
      <div className="flex h-full w-full flex-col">
        {showFace ? (
          <div style={{ height: `${facePct}%` }} className="w-full">
            <CroppedFrame videoSrc={videoSrc} rect={faceCrop ?? FULL_FRAME} className="h-full w-full" />
          </div>
        ) : null}
        <div style={{ height: showFace ? `${100 - facePct}%` : '100%' }} className="w-full">
          <CroppedFrame videoSrc={videoSrc} rect={gameplay} className="h-full w-full" />
        </div>
      </div>
    </div>
  );
}
