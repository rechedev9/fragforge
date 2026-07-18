'use client';

import { useEffect, useState, type ChangeEvent, type FormEvent, type ReactNode } from 'react';
import {
  CheckCircle2,
  FileText,
  ImagePlus,
  Link2,
  LoaderCircle,
  Mic2,
  Save,
  ShieldCheck,
  Trash2,
  Upload,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  deleteNewsVoiceProfile,
  loadNewsDraft,
  loadNewsVoiceProfile,
  saveNewsDraft,
  saveNewsVoiceProfile,
  type NewsShortDraft,
  type NewsVoiceProfile,
} from '@/lib/api/news';

const DEFAULT_SOURCE_URL = 'https://x.com/CounterStrike/status/2077901099961647290';
const DEFAULT_CHANNEL = 'RaizerinhoCS2';
const DEFAULT_TITLE = 'CS2 añade más skins… ¿y el anticheat?';
const DEFAULT_HOOK = '¿OTRA VEZ SKINS? ¿Y EL ANTICHEAT?';
const DEFAULT_SCRIPT = `Valve acaba de anunciar otra actualización para Counter-Strike 2.

¿Un nuevo anticheat? No. Más skins y más pegatinas.

Buscan una colección de armas llamada Fairy Tales y dos colecciones de stickers: Cryptids y Pop Art.

Y en todo el anuncio: cero menciones a VAC, VACnet o VAC Live.

La reacción fue inmediata. Para muchos jugadores, un anticheat nuevo sí que sería un cuento de hadas.

Esto no demuestra que Valve no esté trabajando en él, pero otra vez no hay respuestas públicas.

¿Qué necesita Counter-Strike 2 ahora mismo: más skins o un anticheat que funcione?`;

export function NewsShortWorkspace(): ReactNode {
  const [profile, setProfile] = useState<NewsVoiceProfile | null>();
  const [voiceFile, setVoiceFile] = useState<File | null>(null);
  const [voiceBusy, setVoiceBusy] = useState(false);
  const [voiceError, setVoiceError] = useState('');
  const [sourceUrl, setSourceUrl] = useState(DEFAULT_SOURCE_URL);
  const [channel, setChannel] = useState(DEFAULT_CHANNEL);
  const [title, setTitle] = useState(DEFAULT_TITLE);
  const [hook, setHook] = useState(DEFAULT_HOOK);
  const [script, setScript] = useState(DEFAULT_SCRIPT);
  const [images, setImages] = useState<File[]>([]);
  const [draftSavedAt, setDraftSavedAt] = useState('');
  const [draftError, setDraftError] = useState('');

  useEffect(() => {
    const draft = loadNewsDraft(window.localStorage);
    if (draft !== null) {
      setSourceUrl(draft.sourceUrl);
      setChannel(draft.channel);
      setTitle(draft.title);
      setHook(draft.hook);
      setScript(draft.script);
      setDraftSavedAt(draft.updatedAt);
    }
    void loadNewsVoiceProfile()
      .then((loaded) => {
        setProfile(loaded);
        if (draft === null && loaded !== null) setChannel(loaded.channel);
      })
      .catch((error: unknown) => {
        setProfile(null);
        setVoiceError(error instanceof Error ? error.message : 'No se pudo cargar la voz local.');
      });
  }, []);

  async function uploadVoice(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (voiceFile === null) return;
    setVoiceBusy(true);
    setVoiceError('');
    try {
      const saved = await saveNewsVoiceProfile(voiceFile, channel.trim() || DEFAULT_CHANNEL);
      setProfile(saved);
      setVoiceFile(null);
    } catch (error: unknown) {
      setVoiceError(error instanceof Error ? error.message : 'No se pudo guardar la voz.');
    } finally {
      setVoiceBusy(false);
    }
  }

  async function removeVoice(): Promise<void> {
    if (!window.confirm('¿Eliminar la referencia de voz guardada en este equipo?')) return;
    setVoiceBusy(true);
    setVoiceError('');
    try {
      await deleteNewsVoiceProfile();
      setProfile(null);
      setVoiceFile(null);
    } catch (error: unknown) {
      setVoiceError(error instanceof Error ? error.message : 'No se pudo eliminar la voz.');
    } finally {
      setVoiceBusy(false);
    }
  }

  function selectVoice(event: ChangeEvent<HTMLInputElement>): void {
    setVoiceFile(event.target.files?.[0] ?? null);
  }

  function selectImages(event: ChangeEvent<HTMLInputElement>): void {
    setImages(Array.from(event.target.files ?? []));
  }

  function saveDraft(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    setDraftError('');
    const updatedAt = new Date().toISOString();
    const draft: NewsShortDraft = {
      sourceUrl: sourceUrl.trim(),
      channel: channel.trim() || DEFAULT_CHANNEL,
      title: title.trim(),
      hook: hook.trim(),
      script: script.trim(),
      updatedAt,
    };
    if (!saveNewsDraft(window.localStorage, draft)) {
      setDraftError('No se pudo guardar el borrador en este navegador. Comprueba el almacenamiento local.');
      return;
    }
    setDraftSavedAt(updatedAt);
  }

  return (
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1.55fr)_minmax(340px,0.85fr)]">
      <Card>
        <CardHeader>
          <div className="flex items-start gap-3">
            <FileText className="mt-0.5 size-5 text-primary" aria-hidden />
            <div>
              <CardTitle>Nuevo Short de noticias</CardTitle>
              <CardDescription className="mt-2">
                Guarda la fuente, el enfoque y el guion. El caso de CS2 ya está precargado como plantilla editable.
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <form className="space-y-5" onSubmit={saveDraft}>
            <div className="grid gap-2">
              <Label htmlFor="news-source"><Link2 className="size-4" aria-hidden />Fuente</Label>
              <Input id="news-source" type="url" required value={sourceUrl} onChange={(event) => setSourceUrl(event.target.value)} />
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="news-channel">Canal</Label>
                <Input id="news-channel" required value={channel} onChange={(event) => setChannel(event.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="news-title">Título de YouTube</Label>
                <Input id="news-title" required value={title} onChange={(event) => setTitle(event.target.value)} />
              </div>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="news-hook">Gancho en pantalla</Label>
              <Input id="news-hook" required value={hook} onChange={(event) => setHook(event.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="news-script">Guion</Label>
              <textarea
                id="news-script"
                required
                rows={15}
                maxLength={20000}
                value={script}
                onChange={(event) => setScript(event.target.value)}
                className="min-h-72 w-full resize-y rounded-md border border-input bg-surface/80 px-3.5 py-3 text-[15px] leading-6 outline-none transition focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="news-images"><ImagePlus className="size-4" aria-hidden />Capturas y recursos</Label>
              <Input id="news-images" type="file" accept="image/png,image/jpeg,image/webp" multiple onChange={selectImages} />
              <p className="text-xs text-muted-foreground">
                {images.length === 0 ? 'Añade el post, la noticia oficial y reacciones.' : `${images.length} recurso(s) seleccionado(s) para esta sesión.`}
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-3 pt-2">
              <Button type="submit"><Save aria-hidden />Guardar borrador local</Button>
              {draftSavedAt !== '' ? (
                <span className="inline-flex items-center gap-1.5 text-sm text-emerald-400">
                  <CheckCircle2 className="size-4" aria-hidden />Guardado {new Date(draftSavedAt).toLocaleString('es-ES')}
                </span>
              ) : null}
              {draftError !== '' ? <span role="alert" className="text-sm text-destructive">{draftError}</span> : null}
            </div>
          </form>
        </CardContent>
      </Card>

      <div className="space-y-6">
        <Card>
          <CardHeader>
            <div className="flex items-start gap-3">
              <Mic2 className="mt-0.5 size-5 text-primary" aria-hidden />
              <div>
                <CardTitle>Tu voz</CardTitle>
                <CardDescription className="mt-2">Perfil privado para las narraciones de RaizerinhoCS2.</CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent className="space-y-5">
            {profile === undefined ? (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <LoaderCircle className="size-4 animate-spin" aria-hidden />Cargando perfil local…
              </div>
            ) : null}
            {profile === null ? (
              <p className="text-sm leading-6 text-muted-foreground">Todavía no hay una referencia guardada.</p>
            ) : null}
            {profile !== undefined && profile !== null ? (
              <div className="space-y-4 rounded-lg border border-emerald-500/25 bg-emerald-500/5 p-4">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <p className="font-semibold text-foreground">{profile.name}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {profile.channel} · {profile.locale} · {formatBytes(profile.size_bytes)}
                    </p>
                  </div>
                  <ShieldCheck className="size-5 shrink-0 text-emerald-400" aria-label="Guardada localmente" />
                </div>
                <audio
                  key={profile.updated_at}
                  controls
                  preload="metadata"
                  src={`${profile.audio_url}?v=${encodeURIComponent(profile.updated_at)}`}
                  className="w-full"
                />
                <p className="break-all font-[family-name:var(--font-mono)] text-[10px] text-muted-foreground">
                  SHA-256 {profile.sha256}
                </p>
                <Button type="button" variant="destructive" size="sm" disabled={voiceBusy} onClick={() => void removeVoice()}>
                  <Trash2 aria-hidden />Eliminar voz local
                </Button>
              </div>
            ) : null}

            <form className="space-y-3" onSubmit={(event) => void uploadVoice(event)}>
              <Label htmlFor="voice-reference">{profile === null ? 'Guardar referencia' : 'Reemplazar referencia'}</Label>
              <Input id="voice-reference" type="file" accept="audio/ogg,audio/wav,.ogg,.wav" onChange={selectVoice} />
              <p className="text-xs leading-5 text-muted-foreground">OGG Opus o WAV clásico PCM. Recomendado: entre 10 y 30 segundos, sin música ni ruido. Máximo 25 MB.</p>
              <Button type="submit" variant="secondary" disabled={voiceFile === null || voiceBusy}>
                {voiceBusy ? <LoaderCircle className="animate-spin" aria-hidden /> : <Upload aria-hidden />}
                {profile === null ? 'Guardar voz' : 'Reemplazar voz'}
              </Button>
            </form>
            {voiceError !== '' ? <p role="alert" className="text-sm text-destructive">{voiceError}</p> : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Privacidad</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm leading-6 text-muted-foreground">
            <p>La muestra se guarda dentro de los datos locales de FragForge. No forma parte del repositorio ni del instalador.</p>
            <p>FragForge no la envía a xAI, YouTube ni a ningún proveedor de voz. Puedes escucharla, reemplazarla o eliminarla aquí.</p>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
