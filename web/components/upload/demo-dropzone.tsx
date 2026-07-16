'use client';

import { useCallback, useId, useRef, useState } from 'react';
import { UploadCloud } from 'lucide-react';
import { cn } from '@/lib/utils';

export type DemoDropzoneProps = {
  /** Called with the chosen .dem file(s). The parent owns parsing + navigation. */
  onFiles: (files: File[]) => void;
};

const DEM_EXT = '.dem';

/** Most demos we ever record for one series is a bo5 (5 maps); 10 leaves slack. */
const MAX_FILES = 10;

/**
 * A drop zone + file picker for CS2 .dem files, styled as the mockup's HUD
 * dropzone: dashed cyan border, four corner brackets, glowing circle icon. It
 * accepts a single demo or several at once — a whole bo3/bo5 series — up to
 * {@link MAX_FILES}. The clickable area is a <label> bound to the file input, so
 * the OS file dialog opens natively on click (no JS .click() that can be flaky
 * with hidden inputs). Drag-and-drop and keyboard both work; every file's
 * extension is validated before handing the list up.
 */
export function DemoDropzone({ onFiles }: DemoDropzoneProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const inputId = useId();
  const [dragging, setDragging] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const accept = useCallback(
    (fileList: FileList | null | undefined) => {
      const files = fileList ? Array.from(fileList) : [];
      if (files.length === 0) return;
      if (files.length > MAX_FILES) {
        setError(`Máximo ${MAX_FILES} demos por serie. Has soltado ${files.length}.`);
        return;
      }
      const bad = files.find((f) => !f.name.toLowerCase().endsWith(DEM_EXT));
      if (bad) {
        setError(`"${bad.name}" no es una demo .dem.`);
        return;
      }
      setError(null);
      onFiles(files);
    },
    [onFiles],
  );

  return (
    <div className="flex flex-col gap-3">
      <label
        htmlFor={inputId}
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          accept(e.dataTransfer.files);
        }}
        className={cn(
          'studio-panel studio-panel-raised group relative isolate flex min-h-[268px] cursor-pointer flex-col items-center justify-center overflow-hidden px-6 py-10 text-center transition-[border-color,box-shadow,transform] duration-200 sm:min-h-[300px] sm:px-10 sm:py-12',
          'focus-within:border-primary focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background',
          dragging
            ? 'border-primary ring-2 ring-primary/60 shadow-[0_0_32px_color-mix(in_oklch,var(--primary)_18%,transparent)]'
            : 'hover:border-primary/60 hover:-translate-y-px',
        )}
      >
        <span
          aria-hidden
          className={cn(
            'pointer-events-none absolute inset-2 border border-dashed transition-colors duration-200',
            dragging ? 'border-primary/90 bg-primary/[0.045]' : 'border-primary/30 group-hover:border-primary/60',
          )}
        />
        <span
          aria-hidden
          className="pointer-events-none absolute inset-x-[18%] top-0 h-32 bg-primary/[0.08] opacity-70 blur-[70px] transition-opacity duration-200 group-hover:opacity-100"
        />

        <span className="relative z-10 mb-5 inline-flex items-center gap-2 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.2em] text-primary/85">
          <span className="size-1.5 bg-primary shadow-[0_0_8px_currentColor]" />
          Demo de CS2
        </span>
        <span className="relative z-10 flex size-14 items-center justify-center rounded-full border border-primary/45 bg-background/35 text-primary transition-transform duration-200 [box-shadow:0_0_24px_color-mix(in_oklch,var(--primary)_20%,transparent),inset_0_0_14px_color-mix(in_oklch,var(--primary)_12%,transparent)] group-hover:scale-105 sm:size-16">
          <UploadCloud className="size-6 sm:size-7" />
        </span>
        <span className="relative z-10 mt-4 font-[family-name:var(--font-display)] text-xl font-bold tracking-tight text-foreground sm:text-2xl">
          SUELTA UN .DEM AQUÍ
        </span>
        <span className="relative z-10 mt-2 max-w-lg text-[15px] leading-6 text-muted-foreground">
          Arrastra una demo — o varias, una serie bo3/bo5 completa
        </span>
        <span className="relative z-10 mt-4 inline-flex min-h-10 items-center justify-center border border-primary/40 bg-primary/[0.08] px-5 font-[family-name:var(--font-display)] text-sm font-semibold uppercase tracking-[0.05em] text-primary transition-colors group-hover:border-primary/70 group-hover:bg-primary/[0.12]">
          explora tus archivos
        </span>
        <span className="relative z-10 mt-4 font-[family-name:var(--font-mono)] text-[11px] tracking-[0.16em] text-muted-foreground/80">
          SIN LOGIN · HASTA {MAX_FILES} DEMOS · EL .DEM NUNCA SALE DE TU PC
        </span>

        <input
          id={inputId}
          ref={inputRef}
          type="file"
          multiple
          // No `accept` filter: on Windows the ".dem" filter hid every file in
          // folders without a .dem, so the dialog looked empty/broken. Show all
          // files; the extension check below rejects non-.dem with a message.
          className="sr-only"
          // Reset so picking the same file again still fires onChange.
          onClick={(e) => {
            (e.target as HTMLInputElement).value = '';
          }}
          onChange={(e) => accept(e.target.files)}
        />
      </label>

      {error ? (
        <p role="alert" className="border border-destructive/30 bg-destructive/[0.08] px-4 py-3 text-sm text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}
