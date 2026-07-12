'use client';

import { useCallback, useEffect, useState, type ReactElement } from 'react';
import {
  Clock3,
  Copy,
  Download,
  ExternalLink,
  LoaderCircle,
  RefreshCw,
  Sparkles,
  Tags,
  Youtube,
} from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import {
  upcomingPublishSlots,
  type PublishAssistant,
  type PublishRecommendation,
} from '@/lib/api/publish-assistant';
import type { Video } from '@/lib/api/types';
import {
  copyPublishText,
  downloadPublishMP4,
  initialPublishDraft,
  openYouTubeStudio,
  publishTagsText,
  recommendedPublishDraft,
} from '@/lib/publish-actions';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

const YOUTUBE_TITLE_MAX_LENGTH = 100;
const YOUTUBE_DESCRIPTION_MAX_LENGTH = 5000;
const YOUTUBE_UPLOAD_GUIDE_URL = 'https://support.google.com/youtube/answer/57407?hl=es';

type PublishAssistantDialogProps = {
  open: boolean;
  video: Video;
  onOpenChange(open: boolean): void;
};

function localDayLabel(date: string, timeZone: string): string {
  const instant = new Date(`${date}T12:00:00Z`);
  if (Number.isNaN(instant.getTime())) return date;
  return new Intl.DateTimeFormat('es-ES', {
    timeZone,
    weekday: 'short',
    day: 'numeric',
    month: 'short',
  }).format(instant);
}

function confidenceLabel(confidence: number): string {
  return new Intl.NumberFormat('es-ES', {
    style: 'percent',
    maximumFractionDigits: 0,
  }).format(confidence);
}

function SchedulePanel({ assistant }: { assistant: PublishAssistant }): ReactElement {
  const upcoming = upcomingPublishSlots(assistant.schedule);
  const best = upcoming[0];
  const sourceLinks = [...assistant.schedule.sources, ...assistant.trends.sources].filter(
    (source, index, links) => links.findIndex((candidate) => candidate.url === source.url) === index,
  );
  let trendContent: ReactElement | null = null;
  if (assistant.trends.available && assistant.trends.terms.length > 0) {
    trendContent = (
      <div>
        <p className="text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground">
          Tendencias públicas recientes
        </p>
        <div className="mt-1.5 flex flex-wrap gap-1.5">
          {assistant.trends.terms.map((term) => (
            <span key={term} className="border border-border/70 bg-background/45 px-2 py-1 text-xs text-foreground">
              {term}
            </span>
          ))}
        </div>
      </div>
    );
  } else if (assistant.trends.reason) {
    trendContent = <p className="text-xs leading-relaxed text-muted-foreground">{assistant.trends.reason}</p>;
  }
  return (
    <section className="space-y-3 border border-primary/25 bg-primary/[0.045] p-4" aria-labelledby="publish-schedule-title">
      <div className="flex items-start gap-3">
        <Clock3 className="mt-0.5 size-4 shrink-0 text-primary" />
        <div className="min-w-0">
          <h3 id="publish-schedule-title" className="text-sm font-semibold text-foreground">
            Horario recomendado · {assistant.schedule.timeZone}
          </h3>
          {best ? (
            <>
              <p className="mt-1 text-lg font-bold text-foreground">
                {localDayLabel(best.day.date, assistant.schedule.timeZone)} · {best.slot.localTime}
              </p>
              <p className="text-xs text-muted-foreground">
                Referencia general · confianza {confidenceLabel(best.slot.confidence)}
              </p>
            </>
          ) : (
            <p className="mt-1 text-xs text-muted-foreground">No quedan franjas futuras en este calendario.</p>
          )}
        </div>
      </div>

      {upcoming.length > 0 ? (
        <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-4">
          {upcoming.slice(0, 4).map(({ day, slot }) => (
            <div key={day.date} className="border border-border/70 bg-background/45 px-2.5 py-2 text-xs">
              <span className="block text-muted-foreground">
                {localDayLabel(day.date, assistant.schedule.timeZone)}
              </span>
              <strong className="text-foreground">{slot.localTime}</strong>
            </div>
          ))}
        </div>
      ) : null}

      {trendContent}

      {sourceLinks.length > 0 ? (
        <div className="flex flex-wrap gap-x-3 gap-y-1">
          {sourceLinks.map((source) => (
            <a
              key={source.url}
              href={source.url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-primary underline underline-offset-4"
            >
              {source.title} <ExternalLink className="size-3" />
            </a>
          ))}
        </div>
      ) : null}

      <p className="text-[11px] leading-relaxed text-muted-foreground">{assistant.schedule.caveat}</p>
    </section>
  );
}

export function PublishAssistantDialog({
  open,
  video,
  onOpenChange,
}: PublishAssistantDialogProps): ReactElement {
  const [assistant, setAssistant] = useState<PublishAssistant>();
  const [selectedRecommendation, setSelectedRecommendation] = useState<PublishRecommendation>();
  const [title, setTitle] = useState(video.title);
  const [description, setDescription] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);
    try {
      const next = await api.getPublishAssistant(video.id);
      const draft = initialPublishDraft(next);
      setAssistant(next);
      setSelectedRecommendation(undefined);
      setTitle(draft.title);
      setDescription(draft.description);
      setTags(draft.tags);
    } catch {
      setError('No se pudo preparar la publicación. El MP4 sigue disponible para descargar.');
    } finally {
      setLoading(false);
    }
  }, [video.id]);

  useEffect(() => {
    if (open) void load();
  }, [load, open]);

  function applyRecommendation(recommendation: PublishRecommendation): void {
    const draft = recommendedPublishDraft(recommendation);
    setSelectedRecommendation(recommendation);
    setTitle(draft.title);
    setDescription(draft.description);
    setTags(draft.tags);
  }

  async function copy(value: string, label: string): Promise<void> {
    try {
      await copyPublishText(value);
      toast(`${label} copiado al portapapeles.`);
    } catch {
      toast(`No se pudo copiar ${label.toLowerCase()}.`);
    }
  }

  function download(): void {
    if (video.downloadUrl) downloadPublishMP4(video.downloadUrl, title || video.title);
  }

  const keywords = selectedRecommendation?.keywords ?? assistant?.keywords ?? [];
  const tagsText = publishTagsText(tags);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 uppercase">
            <Youtube className="size-5 text-destructive" /> Preparar publicación
          </DialogTitle>
          <DialogDescription>
            Ajusta los textos, descarga el MP4 y termina el flujo oficial en YouTube Studio.
          </DialogDescription>
        </DialogHeader>

        {loading ? (
          <div className="flex min-h-36 items-center justify-center gap-2 text-sm text-muted-foreground" role="status">
            <LoaderCircle className="size-4 animate-spin" /> Preparando metadatos y horario…
          </div>
        ) : null}

        {!loading && error ? (
          <div className="space-y-3 border border-warning/35 bg-warning/[0.07] p-4" role="alert">
            <p className="text-sm text-warning">{error}</p>
            <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
              <RefreshCw className="size-3.5" /> REINTENTAR
            </Button>
          </div>
        ) : null}

        {!loading && assistant ? (
          <div className="space-y-5">
            <SchedulePanel assistant={assistant} />

            <section className="space-y-2.5" aria-labelledby="publish-title-recommendations">
              <div className="flex items-center gap-2">
                <Sparkles className="size-4 text-primary" />
                <h3 id="publish-title-recommendations" className="text-sm font-semibold text-foreground">
                  Títulos recomendados
                </h3>
              </div>
              <div className="grid gap-2">
                {assistant.recommendations.map((recommendation) => (
                  <Button
                    key={recommendation.title}
                    type="button"
                    variant="outline"
                    className="h-auto justify-between gap-3 whitespace-normal px-3 py-2.5 text-left"
                    onClick={() => applyRecommendation(recommendation)}
                    aria-label={`Usar título recomendado: ${recommendation.title}`}
                    aria-pressed={selectedRecommendation?.title === recommendation.title}
                  >
                    <span>{recommendation.title}</span>
                    <span className="shrink-0 font-[family-name:var(--font-mono)] text-[10px] text-muted-foreground">
                      {Math.round(recommendation.score)}/100
                    </span>
                  </Button>
                ))}
              </div>
              {selectedRecommendation ? (
                <p className="border border-border/70 bg-surface/50 p-3 text-xs text-muted-foreground" role="status">
                  {selectedRecommendation.rationale}
                </p>
              ) : null}
            </section>

            <div className="space-y-2">
              <div className="flex items-center justify-between gap-3">
                <Label htmlFor="publish-title">Título</Label>
                <Button type="button" variant="ghost" size="sm" onClick={() => void copy(title, 'Título')}>
                  <Copy className="size-3.5" /> COPIAR
                </Button>
              </div>
              <Input
                id="publish-title"
                value={title}
                onChange={(event) => setTitle(event.currentTarget.value)}
                maxLength={YOUTUBE_TITLE_MAX_LENGTH}
              />
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between gap-3">
                <Label htmlFor="publish-description">Descripción</Label>
                <Button type="button" variant="ghost" size="sm" onClick={() => void copy(description, 'Descripción')}>
                  <Copy className="size-3.5" /> COPIAR
                </Button>
              </div>
              <textarea
                id="publish-description"
                value={description}
                onChange={(event) => setDescription(event.currentTarget.value)}
                maxLength={YOUTUBE_DESCRIPTION_MAX_LENGTH}
                rows={6}
                className="w-full resize-y rounded-md border border-input bg-surface/80 px-3.5 py-3 text-sm text-foreground shadow-xs outline-none transition-[border-color,box-shadow,background-color] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40"
              />
            </div>

            <section className="space-y-2 border border-border/70 bg-surface/40 p-3" aria-labelledby="publish-tags-title">
              <div className="flex items-center justify-between gap-3">
                <h3 id="publish-tags-title" className="flex items-center gap-2 text-sm font-semibold text-foreground">
                  <Tags className="size-4 text-primary" /> Etiquetas
                </h3>
                <Button type="button" variant="ghost" size="sm" onClick={() => void copy(tagsText, 'Etiquetas')}>
                  <Copy className="size-3.5" /> COPIAR
                </Button>
              </div>
              <p className="text-sm text-foreground">{tagsText}</p>
              {keywords.length > 0 ? (
                <p className="text-xs text-muted-foreground">
                  <strong className="text-foreground">Palabras clave:</strong> {keywords.join(' · ')}
                </p>
              ) : null}
            </section>
          </div>
        ) : null}

        <div className="border border-border/70 bg-background/45 p-3 text-xs leading-relaxed text-muted-foreground">
          YouTube Studio te guiará por <strong className="text-foreground">CREAR → Subir vídeos</strong>, audiencia,
          visibilidad y programación. Consulta la{' '}
          <a
            href={YOUTUBE_UPLOAD_GUIDE_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary underline underline-offset-4"
          >
            guía oficial de YouTube
          </a>.
        </div>

        <DialogFooter className="flex-col gap-2 sm:flex-row sm:justify-between">
          <Button type="button" variant="outline" onClick={download} disabled={!video.downloadUrl}>
            <Download className="size-4" /> DESCARGAR MP4
          </Button>
          <Button type="button" onClick={openYouTubeStudio}>
            <Youtube className="size-4" /> ABRIR YOUTUBE STUDIO
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
