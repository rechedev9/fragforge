# Research — Sincronización con música (librosa + FFmpeg)

Referencia: librosa docs https://librosa.org/doc/

## Objetivo

Dado un set de segmentos compuestos (con efectos ya aplicados) y una pista musical, alinear los puntos de corte y los efectos al beat. Resultado: el clip "se siente" editado.

## Pipeline

1. **Detectar tempo y beats de la pista** con `librosa.beat.beat_track`.
2. **Detectar onsets** (energy spikes) con `librosa.onset.onset_detect` — útiles para alinear con drops.
3. **Reordenar / recortar segmentos** para que cada corte caiga en un beat (snap-to-beat).
4. **Mezclar la música** con el audio del juego usando FFmpeg sidechain ducking (ver `04-post-processing.md`).

## Detección de beats

```python
import librosa

y, sr = librosa.load("track.wav")
tempo, beat_frames = librosa.beat.beat_track(y=y, sr=sr)
beat_times = librosa.frames_to_time(beat_frames, sr=sr)
# beat_times: array de segundos en los que cae cada beat
```

Para tracks de "música viral" (típicamente trap / phonk / hyperpop, ~140–180 BPM, beats marcados), el beat tracker default funciona bien. Si la pista tiene un tempo conocido, se puede pasar `bpm=140` y mejora la estabilidad.

## Snap-to-beat

```python
def snap_segment_to_beats(segment_duration, beat_times, start_offset):
    end_target = start_offset + segment_duration
    # encontrar el beat más cercano a end_target dentro de ±0.25s
    candidates = beat_times[(beat_times >= end_target - 0.25) & (beat_times <= end_target + 0.25)]
    if len(candidates) == 0:
        return segment_duration  # no encontrado, dejar como está
    return candidates[0] - start_offset
```

El composer puede ajustar el `tick_end` del segmento original ligeramente para que el corte caiga en el beat. Si el desfase necesario excede el `post_roll` disponible, el segmento se deja sin snap (mejor un corte ligeramente off que cortar la kill).

## Alineación de efectos con beats

Para efectos como "drop" / "freeze frame" en un beat fuerte:

```python
onset_env = librosa.onset.onset_strength(y=y, sr=sr)
# encontrar el onset con mayor energía (probable drop)
strongest_onset_idx = np.argmax(onset_env)
drop_time = librosa.frames_to_time(strongest_onset_idx, sr=sr)
```

El "drop" puede ser el momento en el que disparamos el zoom en la kill clave del clip.

## Selección automática de pista

V1: el usuario provee la pista (sube un mp3 o elige de su catálogo).
V2: librería de pistas pre-categorizadas por mood/tempo + selección automática según la composición del clip (todo pistola → energía alta; todo AWP → más cinemático). No es V1.

## Limitaciones a tener presentes

- librosa carga audio en memoria; para pistas largas hay que limitar a la duración del clip.
- El beat tracker puede fallar en pistas con tempo variable. Estrategia de fallback: si la confianza es baja, NO hacer snap-to-beat (mejor dejar los segmentos sin alinear que alinear mal).
- Sample rate por defecto en `librosa.load` es 22050 Hz; para producción usar `sr=None` o `sr=44100` y luego pasar a FFmpeg con el mismo SR.
