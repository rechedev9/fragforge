'use client';

import { useCallback, useEffect, useRef } from 'react';
import type { NormalizedRect } from '@/lib/api/streams';
import { cn } from '@/lib/utils';

const MIN_SIZE = 0.08;

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/** Keeps the rect inside 0..1 on both axes and never smaller than MIN_SIZE. */
function clampRect(rect: NormalizedRect): NormalizedRect {
  const width = clamp(rect.width, MIN_SIZE, 1);
  const height = clamp(rect.height, MIN_SIZE, 1);
  const x = clamp(rect.x, 0, 1 - width);
  const y = clamp(rect.y, 0, 1 - height);
  return { x, y, width, height };
}

type Drag = { kind: 'move' | 'resize'; startClientX: number; startClientY: number; startRect: NormalizedRect };

/**
 * Facecam picker: a paused <video> (the job's source proxy URL) with an
 * absolutely-positioned draggable/resizable rectangle overlay, implemented
 * with pointer events (no new deps). The rect is normalized (0..1 of the
 * source frame) both in and out, so it maps directly onto the edit plan's
 * face_crop with no extra pixel bookkeeping at the call site: a pointer-move
 * delta in CSS pixels is divided by the container's current bounding-box
 * width/height to get a normalized delta, which is then added to the rect
 * that was in effect when the drag started and clamped back into 0..1.
 */
export function FacecamPicker({
  videoSrc,
  rect,
  onChange,
  disabled = false,
}: {
  videoSrc: string;
  rect: NormalizedRect;
  onChange: (rect: NormalizedRect) => void;
  disabled?: boolean;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const dragRef = useRef<Drag | null>(null);

  // Seek to a representative frame (mid-clip) once metadata loads, then pause
  // there so the picker shows a real frame instead of a black/first frame.
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const onLoaded = () => {
      video.currentTime = video.duration ? Math.min(video.duration / 2, video.duration - 0.1) : 0;
    };
    const onSeeked = () => video.pause();
    video.addEventListener('loadedmetadata', onLoaded);
    video.addEventListener('seeked', onSeeked);
    return () => {
      video.removeEventListener('loadedmetadata', onLoaded);
      video.removeEventListener('seeked', onSeeked);
    };
  }, [videoSrc]);

  const normalizedDelta = useCallback((clientX: number, clientY: number, drag: Drag) => {
    const el = containerRef.current;
    if (!el) return { dx: 0, dy: 0 };
    const box = el.getBoundingClientRect();
    return {
      dx: box.width > 0 ? (clientX - drag.startClientX) / box.width : 0,
      dy: box.height > 0 ? (clientY - drag.startClientY) / box.height : 0,
    };
  }, []);

  const beginDrag = useCallback(
    (kind: Drag['kind']) => (e: React.PointerEvent<HTMLDivElement>) => {
      if (disabled) return;
      e.preventDefault();
      e.stopPropagation();
      e.currentTarget.setPointerCapture(e.pointerId);
      dragRef.current = { kind, startClientX: e.clientX, startClientY: e.clientY, startRect: rect };
    },
    [disabled, rect],
  );

  const onPointerMove = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      const drag = dragRef.current;
      if (!drag) return;
      const { dx, dy } = normalizedDelta(e.clientX, e.clientY, drag);
      if (drag.kind === 'move') {
        onChange(clampRect({ ...drag.startRect, x: drag.startRect.x + dx, y: drag.startRect.y + dy }));
      } else {
        onChange(clampRect({ ...drag.startRect, width: drag.startRect.width + dx, height: drag.startRect.height + dy }));
      }
    },
    [normalizedDelta, onChange],
  );

  const endDrag = useCallback(() => {
    dragRef.current = null;
  }, []);

  return (
    <div
      ref={containerRef}
      className="relative aspect-video w-full touch-none overflow-hidden rounded-lg border border-border bg-black select-none"
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    >
      <video
        ref={videoRef}
        src={videoSrc}
        muted
        playsInline
        preload="auto"
        className="pointer-events-none absolute inset-0 h-full w-full object-contain"
      />
      <div
        role="button"
        tabIndex={disabled ? -1 : 0}
        aria-label="Facecam crop region"
        onPointerDown={beginDrag('move')}
        className={cn(
          'absolute rounded-sm border-2 border-primary bg-primary/10 shadow-[0_0_0_9999px_rgba(0,0,0,0.45)]',
          disabled ? 'pointer-events-none opacity-40' : 'cursor-move',
        )}
        style={{
          left: `${rect.x * 100}%`,
          top: `${rect.y * 100}%`,
          width: `${rect.width * 100}%`,
          height: `${rect.height * 100}%`,
        }}
      >
        <div
          role="button"
          aria-label="Resize facecam crop"
          onPointerDown={beginDrag('resize')}
          className="absolute -right-1.5 -bottom-1.5 size-4 cursor-nwse-resize rounded-sm border-2 border-background bg-primary"
        />
      </div>
    </div>
  );
}
