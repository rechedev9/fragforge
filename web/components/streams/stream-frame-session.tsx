'use client';

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react';
import type { NormalizedRect } from '@/lib/api/streams';
import { browserWindowActivity } from '@/lib/window-activity';
import { LatestFrameRequest } from '@/lib/stream-frame-session';

const HAVE_METADATA = 1;
const HAVE_CURRENT_DATA = 2;
const MAX_CANVAS_WIDTH = 720;

interface StreamFrameState {
  requestSnapshot(
    seconds: number,
    rect: NormalizedRect,
    outputWidth: number,
    outputHeight: number,
  ): Promise<ImageBitmap | null>;
  revision: number;
  sourceHeight: number;
  sourceWidth: number;
  video: HTMLVideoElement | null;
}

const StreamFrameContext = createContext<StreamFrameState | null>(null);

interface SnapshotRequest {
  outputHeight: number;
  outputWidth: number;
  rect: NormalizedRect;
  resolve: (bitmap: ImageBitmap | null) => void;
  seconds: number;
}

export function StreamFrameSession({
  children,
  frameSeconds,
  onMediaError,
  videoSrc,
}: {
  children: ReactNode;
  frameSeconds: number;
  onMediaError?: () => void;
  videoSrc: string;
}): ReactElement {
  const videoRef = useRef<HTMLVideoElement>(null);
  const desiredSecondsRef = useRef(frameSeconds);
  const requestsRef = useRef(new LatestFrameRequest());
  const activeRef = useRef(browserWindowActivity.isActive());
  const seekingRef = useRef(false);
  const snapshotInFlightRef = useRef<SnapshotRequest | null>(null);
  const snapshotQueueRef = useRef<SnapshotRequest[]>([]);
  const scheduledRef = useRef<number | null>(null);
  const [activityRevision, setActivityRevision] = useState(0);
  const [state, setState] = useState<StreamFrameState>({
    requestSnapshot: async () => null,
    revision: 0,
    sourceHeight: 0,
    sourceWidth: 0,
    video: null,
  });

  const requestSnapshot = useCallback((
    seconds: number,
    rect: NormalizedRect,
    outputWidth: number,
    outputHeight: number,
  ): Promise<ImageBitmap | null> => new Promise((resolve) => {
    for (const obsolete of snapshotQueueRef.current.splice(0)) obsolete.resolve(null);
    snapshotQueueRef.current.push({ outputHeight, outputWidth, rect, resolve, seconds });
    const video = videoRef.current;
    if (video) startNextSnapshot(video);
  }), []);

  function startNextSnapshot(video: HTMLVideoElement): boolean {
    if (!activeRef.current || seekingRef.current || snapshotInFlightRef.current !== null) return false;
    const request = snapshotQueueRef.current.shift();
    if (!request) return false;
    snapshotInFlightRef.current = request;
    const target = Math.min(
      Math.max(0, Number.isFinite(request.seconds) ? request.seconds : 0),
      Math.max(0, video.duration - 0.001),
    );
    if (Math.abs(video.currentTime - target) <= 0.005) {
      void finishSnapshot(video, request);
      return true;
    }
    seekingRef.current = true;
    video.currentTime = target;
    return true;
  }

  async function finishSnapshot(video: HTMLVideoElement, request: SnapshotRequest): Promise<void> {
    let bitmap: ImageBitmap | null = null;
    try {
      const canvas = document.createElement('canvas');
      canvas.width = Math.max(1, Math.min(MAX_CANVAS_WIDTH, Math.round(request.outputWidth)));
      canvas.height = Math.max(1, Math.round(canvas.width * request.outputHeight / request.outputWidth));
      const context = canvas.getContext('2d', { alpha: false });
      if (context) {
        context.drawImage(
          video,
          request.rect.x * video.videoWidth,
          request.rect.y * video.videoHeight,
          request.rect.width * video.videoWidth,
          request.rect.height * video.videoHeight,
          0,
          0,
          canvas.width,
          canvas.height,
        );
        bitmap = await createImageBitmap(canvas);
      }
    } catch {
      bitmap = null;
    }
    request.resolve(bitmap);
    if (snapshotInFlightRef.current === request) snapshotInFlightRef.current = null;
    seekingRef.current = false;
    if (startNextSnapshot(video)) return;
    requestsRef.current.reset(desiredSecondsRef.current);
    const liveTarget = requestsRef.current.next(video.currentTime, video.duration);
    if (liveTarget !== null) {
      seekingRef.current = true;
      video.currentTime = liveTarget;
      return;
    }
    setState((current) => ({ ...current, revision: current.revision + 1 }));
  }

  useEffect(() => {
    desiredSecondsRef.current = frameSeconds;
    const video = videoRef.current;
    if (!video || !activeRef.current || video.readyState < HAVE_METADATA) return;
    scheduleSeek(video);

    function scheduleSeek(target: HTMLVideoElement): void {
      if (scheduledRef.current !== null) cancelAnimationFrame(scheduledRef.current);
      scheduledRef.current = requestAnimationFrame(() => {
        scheduledRef.current = null;
        if (!activeRef.current || seekingRef.current) return;
        requestsRef.current.request(desiredSecondsRef.current);
        const next = requestsRef.current.next(target.currentTime, target.duration);
        target.pause();
        if (next === null) {
          setState((current) => ({ ...current, revision: current.revision + 1 }));
          return;
        }
        seekingRef.current = true;
        target.currentTime = next;
      });
    }
  }, [activityRevision, frameSeconds, videoSrc]);

  useEffect(() => {
    const video = videoRef.current;
    const snapshotQueue = snapshotQueueRef.current;
    const onActivity = (): void => {
      activeRef.current = browserWindowActivity.isActive();
      if (!activeRef.current) {
        video?.pause();
        if (scheduledRef.current !== null) cancelAnimationFrame(scheduledRef.current);
        scheduledRef.current = null;
        return;
      }
      if (video && video.readyState >= HAVE_METADATA) {
        if (startNextSnapshot(video)) return;
        setActivityRevision((current) => current + 1);
      }
    };
    const unsubscribe = browserWindowActivity.subscribe(onActivity);
    return () => {
      unsubscribe();
      if (scheduledRef.current !== null) cancelAnimationFrame(scheduledRef.current);
      video?.pause();
      video?.removeAttribute('src');
      video?.load();
      snapshotInFlightRef.current?.resolve(null);
      snapshotInFlightRef.current = null;
      for (const request of snapshotQueue.splice(0)) request.resolve(null);
    };
  }, []);

  return (
    <StreamFrameContext.Provider value={{ ...state, requestSnapshot }}>
      <video
        ref={videoRef}
        src={videoSrc}
        muted
        playsInline
        preload="metadata"
        aria-hidden="true"
        data-stream-frame="shared-decoder"
        className="hidden"
        onError={onMediaError}
        onLoadedMetadata={(event) => {
          const video = event.currentTarget;
          video.pause();
          requestsRef.current.reset(desiredSecondsRef.current);
          setState((current) => ({
            ...current,
            revision: current.revision + 1,
            sourceHeight: video.videoHeight,
            sourceWidth: video.videoWidth,
            video,
          }));
          const requested = requestsRef.current.next(video.currentTime, video.duration);
          if (requested !== null) {
            seekingRef.current = true;
            video.currentTime = requested;
          } else if (video.readyState < HAVE_CURRENT_DATA && video.duration > 0) {
            // loadedmetadata does not promise a drawable frame. A tiny seek
            // forces Chromium to decode the first frame without preloading
            // the full source.
            seekingRef.current = true;
            video.currentTime = Math.min(0.001, Math.max(0, video.duration - 0.001));
          }
        }}
        onLoadedData={() => {
          if (activeRef.current) setState((current) => ({ ...current, revision: current.revision + 1 }));
        }}
        onSeeked={(event) => {
          event.currentTarget.pause();
          const snapshotRequest = snapshotInFlightRef.current;
          if (snapshotRequest !== null) {
            if (!activeRef.current) {
              seekingRef.current = false;
              snapshotInFlightRef.current = null;
              snapshotQueueRef.current.unshift(snapshotRequest);
              return;
            }
            void finishSnapshot(event.currentTarget, snapshotRequest);
            return;
          }
          seekingRef.current = false;
          requestsRef.current.request(desiredSecondsRef.current);
          if (!activeRef.current) {
            requestsRef.current.reset(desiredSecondsRef.current);
            return;
          }
          const requested = requestsRef.current.settled(event.currentTarget.currentTime, event.currentTarget.duration);
          setState((current) => ({ ...current, revision: current.revision + 1 }));
          if (snapshotQueueRef.current.length > 0) {
            // The snapshot temporarily preempts the coalesced live seek. Reset
            // its in-flight marker so finishing the snapshot can seek back to
            // the latest preview time instead of leaving the decoder stuck.
            requestsRef.current.reset(desiredSecondsRef.current);
            if (startNextSnapshot(event.currentTarget)) return;
          }
          if (requested !== null) {
            seekingRef.current = true;
            event.currentTarget.currentTime = requested;
          }
        }}
      />
      {children}
    </StreamFrameContext.Provider>
  );
}

export function useStreamFrame(): StreamFrameState {
  const state = useContext(StreamFrameContext);
  if (state === null) throw new Error('StreamFrameCanvas must be rendered inside StreamFrameSession');
  return state;
}

export function StreamFrameCanvas({
  className,
  mode,
  outputHeight,
  outputWidth,
  rect,
}: {
  className?: string;
  mode: 'contain' | 'cover' | 'stretch';
  outputHeight?: number;
  outputWidth?: number;
  rect?: NormalizedRect;
}): ReactElement {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const frame = useStreamFrame();

  useEffect(() => {
    const canvas = canvasRef.current;
    const video = frame.video;
    if (!canvas
      || !video
      || video.readyState < HAVE_CURRENT_DATA
      || frame.sourceWidth <= 0
      || frame.sourceHeight <= 0) return;
    const draw = (): void => {
      const box = canvas.getBoundingClientRect();
      if (box.width <= 0 || box.height <= 0) return;
      const width = Math.max(1, Math.min(MAX_CANVAS_WIDTH, Math.round(box.width)));
      const height = Math.max(1, Math.round(width * box.height / box.width));
      if (canvas.width !== width) canvas.width = width;
      if (canvas.height !== height) canvas.height = height;
      const context = canvas.getContext('2d', { alpha: false });
      if (!context) return;
      context.fillStyle = '#000';
      context.fillRect(0, 0, width, height);
      const crop = rect ?? { x: 0, y: 0, width: 1, height: 1 };
      let sx = crop.x * frame.sourceWidth;
      let sy = crop.y * frame.sourceHeight;
      let sw = crop.width * frame.sourceWidth;
      let sh = crop.height * frame.sourceHeight;
      if (mode === 'cover') {
        const targetAspect = (outputWidth ?? width) / (outputHeight ?? height);
        const sourceAspect = sw / sh;
        if (sourceAspect > targetAspect) {
          const nextWidth = sh * targetAspect;
          sx += (sw - nextWidth) / 2;
          sw = nextWidth;
        } else {
          const nextHeight = sw / targetAspect;
          sy += (sh - nextHeight) / 2;
          sh = nextHeight;
        }
      }
      try {
        context.drawImage(video, sx, sy, sw, sh, 0, 0, width, height);
      } catch {
        // Metadata can race decoded frame availability. loadeddata/seeked
        // publishes another revision once Chromium has a drawable frame.
      }
    };
    draw();
    const observer = new ResizeObserver(draw);
    observer.observe(canvas);
    return () => observer.disconnect();
  }, [frame, mode, outputHeight, outputWidth, rect]);

  return <canvas ref={canvasRef} className={className} aria-hidden="true" data-stream-frame-canvas={mode} />;
}
