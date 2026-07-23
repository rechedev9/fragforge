'use client';

import { useCallback, useId, useRef, type ReactNode } from 'react';
import type { NormalizedRect } from '@/lib/api/streams';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';
import { StreamFrameCanvas, useStreamFrame } from '@/components/streams/stream-frame-session';

const DEFAULT_MIN_SIZE = 0.08;
const MIN_NORMALIZED_SIZE = 0.001;
const KEYBOARD_STEP = 0.005;
const KEYBOARD_LARGE_STEP = 0.02;
const SCRUBBER_STEP_SECONDS = 0.01;

type CropPickerKind = 'facecam' | 'killfeed';
type Drag = { kind: 'move' | 'resize'; startClientX: number; startClientY: number; startRect: NormalizedRect };

const PICKER_LABELS: Record<CropPickerKind, { move: string; resize: string }> = {
  facecam: {
    move: 'Mover región de recorte del facecam',
    resize: 'Redimensionar región de recorte del facecam',
  },
  killfeed: {
    move: 'Mover región de recorte de la killfeed',
    resize: 'Redimensionar región de recorte de la killfeed',
  },
};

const PICKER_ACCENTS: Record<CropPickerKind, { region: string; handle: string; range: string }> = {
  facecam: {
    region: 'border-primary bg-primary/10 focus-visible:ring-primary',
    handle: 'border-background bg-primary focus-visible:ring-primary',
    range: 'accent-primary',
  },
  killfeed: {
    region: 'border-stream bg-stream/10 focus-visible:ring-stream',
    handle: 'border-background bg-stream focus-visible:ring-stream',
    range: 'accent-stream',
  },
};

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/** Keeps the rect inside 0..1 on both axes and respects independent minimum dimensions. */
function clampRect(rect: NormalizedRect, minWidth: number, minHeight: number): NormalizedRect {
  const width = clamp(rect.width, minWidth, 1);
  const height = clamp(rect.height, minHeight, 1);
  const x = clamp(rect.x, 0, 1 - width);
  const y = clamp(rect.y, 0, 1 - height);
  return { x, y, width, height };
}

function formatTimestamp(seconds: number): string {
  const safeSeconds = Number.isFinite(seconds) ? Math.max(0, seconds) : 0;
  const minutes = Math.floor(safeSeconds / 60);
  const remainder = safeSeconds - minutes * 60;
  return `${minutes}:${remainder.toFixed(2).padStart(5, '0')}`;
}

/**
 * Reusable source crop picker. Pointer and keyboard controls both emit a
 * normalized rectangle, while an optional controlled scrubber lets multiple
 * pickers and the 9:16 preview share one absolute source timestamp.
 */
export function CropPicker({
  rect,
  onChange,
  kind,
  frameSeconds,
  durationSeconds,
  onFrameSecondsChange,
  showScrubber = false,
  minWidth = DEFAULT_MIN_SIZE,
  minHeight = DEFAULT_MIN_SIZE,
  disabled = false,
}: {
  rect: NormalizedRect;
  onChange: (rect: NormalizedRect) => void;
  kind: CropPickerKind;
  frameSeconds: number;
  durationSeconds?: number;
  onFrameSecondsChange?: (seconds: number) => void;
  showScrubber?: boolean;
  minWidth?: number;
  minHeight?: number;
  disabled?: boolean;
}): ReactNode {
  const containerRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<Drag | null>(null);
  const frame = useStreamFrame();
  const sourceAspectRatio = frame.sourceWidth > 0 && frame.sourceHeight > 0
    ? `${frame.sourceWidth} / ${frame.sourceHeight}`
    : null;
  const instructionsId = useId();
  const scrubberId = useId();
  const safeMinWidth = Number.isFinite(minWidth)
    ? clamp(minWidth, MIN_NORMALIZED_SIZE, 1)
    : DEFAULT_MIN_SIZE;
  const safeMinHeight = Number.isFinite(minHeight)
    ? clamp(minHeight, MIN_NORMALIZED_SIZE, 1)
    : DEFAULT_MIN_SIZE;
  const safeRect = clampRect(rect, safeMinWidth, safeMinHeight);
  const labels = PICKER_LABELS[kind];
  const accents = PICKER_ACCENTS[kind];
  const providedDuration = durationSeconds ?? 0;
  const safeDuration =
    Number.isFinite(providedDuration) && providedDuration > 0
      ? providedDuration
      : 0;
  const scrubberValue = clamp(
    Number.isFinite(frameSeconds) ? frameSeconds : 0,
    0,
    safeDuration,
  );

  const normalizedDelta = useCallback((clientX: number, clientY: number, drag: Drag) => {
    const container = containerRef.current;
    if (!container) return { dx: 0, dy: 0 };
    const box = container.getBoundingClientRect();
    return {
      dx: box.width > 0 ? (clientX - drag.startClientX) / box.width : 0,
      dy: box.height > 0 ? (clientY - drag.startClientY) / box.height : 0,
    };
  }, []);

  const beginDrag = useCallback(
    (dragKind: Drag['kind']) => (event: React.PointerEvent<HTMLButtonElement>) => {
      if (disabled) return;
      event.preventDefault();
      event.stopPropagation();
      event.currentTarget.setPointerCapture(event.pointerId);
      dragRef.current = {
        kind: dragKind,
        startClientX: event.clientX,
        startClientY: event.clientY,
        startRect: safeRect,
      };
    },
    [disabled, safeRect],
  );

  const onPointerMove = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      const drag = dragRef.current;
      if (!drag) return;
      const { dx, dy } = normalizedDelta(event.clientX, event.clientY, drag);
      if (drag.kind === 'move') {
        onChange(clampRect(
          { ...drag.startRect, x: drag.startRect.x + dx, y: drag.startRect.y + dy },
          safeMinWidth,
          safeMinHeight,
        ));
        return;
      }
      onChange(clampRect(
        { ...drag.startRect, width: drag.startRect.width + dx, height: drag.startRect.height + dy },
        safeMinWidth,
        safeMinHeight,
      ));
    },
    [normalizedDelta, onChange, safeMinHeight, safeMinWidth],
  );

  const endDrag = useCallback(() => {
    dragRef.current = null;
  }, []);

  const moveWithKeyboard = useCallback(
    (event: React.KeyboardEvent<HTMLButtonElement>) => {
      const step = event.shiftKey ? KEYBOARD_LARGE_STEP : KEYBOARD_STEP;
      let next: NormalizedRect | null = null;
      if (event.key === 'ArrowLeft') next = { ...safeRect, x: safeRect.x - step };
      if (event.key === 'ArrowRight') next = { ...safeRect, x: safeRect.x + step };
      if (event.key === 'ArrowUp') next = { ...safeRect, y: safeRect.y - step };
      if (event.key === 'ArrowDown') next = { ...safeRect, y: safeRect.y + step };
      if (!next) return;
      event.preventDefault();
      onChange(clampRect(next, safeMinWidth, safeMinHeight));
    },
    [onChange, safeMinHeight, safeMinWidth, safeRect],
  );

  const resizeWithKeyboard = useCallback(
    (event: React.KeyboardEvent<HTMLButtonElement>) => {
      const step = event.shiftKey ? KEYBOARD_LARGE_STEP : KEYBOARD_STEP;
      let next: NormalizedRect | null = null;
      if (event.key === 'ArrowLeft') next = { ...safeRect, width: safeRect.width - step };
      if (event.key === 'ArrowRight') next = { ...safeRect, width: safeRect.width + step };
      if (event.key === 'ArrowUp') next = { ...safeRect, height: safeRect.height - step };
      if (event.key === 'ArrowDown') next = { ...safeRect, height: safeRect.height + step };
      if (!next) return;
      event.preventDefault();
      onChange(clampRect(next, safeMinWidth, safeMinHeight));
    },
    [onChange, safeMinHeight, safeMinWidth, safeRect],
  );

  return (
    <div className="flex flex-col gap-3" data-stream-crop-picker={kind}>
      <p id={instructionsId} className="sr-only">
        Usa las flechas para ajustar el recorte. Mantén Mayús para mover o redimensionar más rápido.
      </p>
      <div
        ref={containerRef}
        className="relative aspect-video w-full touch-none overflow-hidden rounded-lg border border-border bg-background select-none"
        style={sourceAspectRatio ? { aspectRatio: sourceAspectRatio } : undefined}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <StreamFrameCanvas
          mode="contain"
          className="pointer-events-none absolute inset-0 h-full w-full object-contain"
        />
        <button
          type="button"
          disabled={disabled}
          aria-label={labels.move}
          aria-describedby={instructionsId}
          onPointerDown={beginDrag('move')}
          onKeyDown={moveWithKeyboard}
          className={cn(
            'absolute cursor-move rounded-sm border-2 shadow-[0_0_0_9999px_color-mix(in_oklch,var(--background)_70%,transparent)] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-default disabled:opacity-40',
            accents.region,
          )}
          style={{
            left: `${safeRect.x * 100}%`,
            top: `${safeRect.y * 100}%`,
            width: `${safeRect.width * 100}%`,
            height: `${safeRect.height * 100}%`,
          }}
        />
        <button
          type="button"
          disabled={disabled}
          aria-label={labels.resize}
          aria-describedby={instructionsId}
          onPointerDown={beginDrag('resize')}
          onKeyDown={resizeWithKeyboard}
          className={cn(
            'absolute size-4 -translate-x-1/2 -translate-y-1/2 cursor-nwse-resize rounded-sm border-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-default disabled:opacity-40',
            accents.handle,
          )}
          style={{
            left: `${(safeRect.x + safeRect.width) * 100}%`,
            top: `${(safeRect.y + safeRect.height) * 100}%`,
          }}
        />
      </div>

      {showScrubber ? (
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor={scrubberId} className="text-xs text-muted-foreground">
              Fotograma compartido del vídeo
            </Label>
            <output
              htmlFor={scrubberId}
              className="font-[family-name:var(--font-mono)] text-[11px] tabular-nums text-stream"
            >
              {formatTimestamp(scrubberValue)} / {formatTimestamp(safeDuration)}
            </output>
          </div>
          <input
            id={scrubberId}
            type="range"
            min={0}
            max={safeDuration}
            step={SCRUBBER_STEP_SECONDS}
            value={scrubberValue}
            disabled={disabled || safeDuration <= 0 || !onFrameSecondsChange}
            aria-label="Tiempo compartido del vídeo para seleccionar la killfeed"
            aria-valuetext={`${formatTimestamp(scrubberValue)} de ${formatTimestamp(safeDuration)}`}
            onChange={(event) => onFrameSecondsChange?.(Number(event.target.value))}
            className={cn('w-full disabled:cursor-not-allowed disabled:opacity-50', accents.range)}
          />
          {safeDuration <= 0 ? (
            <p className="text-xs text-warning">La duración del vídeo todavía no está disponible.</p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
