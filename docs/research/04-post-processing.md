# Research — Post-procesado con FFmpeg

Referencias clave (docs FFmpeg oficiales):
- Filtro `concat`: https://ffmpeg.org/ffmpeg-all.html#concat
- Filtro `zoompan`: https://ffmpeg.org/ffmpeg-all.html#zoompan
- Filtro `overlay`: https://ffmpeg.org/ffmpeg-all.html#overlay
- Filtro `amix` / `sidechaincompress` para mezcla de audio.

## Pipeline de composición por segmento

Cada segmento crudo de HLAE entra al Effects Composer junto con la metadata de las kills que contiene. El composer:

1. **Trim / re-mark** de inicio y fin precisos (HLAE puede haber grabado con margen).
2. **Aplicar efectos** por kill según las reglas Lua.
3. **Color grade** ligero (LUT del preset).
4. Emitir un clip "composed" sin música todavía.

## Efectos típicos en FFmpeg

### Zoom tras kill (estilo AWP)

`zoompan` permite zoom dinámico, pero la sintaxis es engorrosa. Para zoom "in-and-out" en un punto temporal preciso, esto suele ser más limpio:

```
[0:v]split[base][zoom];
[zoom]crop=iw/1.4:ih/1.4:(iw-iw/1.4)/2:(ih-ih/1.4)/2,scale=iw:ih[zoomed];
[base][zoomed]overlay=enable='between(t,5.0,6.0)'[v]
```

(Pseudo, hay que afinar curva de easing; en Python con `ffmpeg-python` esto se construye programáticamente.)

### Flash blanco (post-kill de pistola)

```
[0:v]drawbox=x=0:y=0:w=iw:h=ih:color=white@1.0:t=fill:enable='between(t,2.0,2.15)'[v]
```

O mejor con un fade rápido in/out vía `geq` para suavizar.

### Slow motion (en headshot)

```
[0:v]setpts=2.5*PTS,trim=start=4.5:end=5.5[slow]; ...
[0:a]atempo=0.4[as]; ...
```

Y luego concat con la parte normal del clip.

### Color grade

```
-vf "curves=preset=increase_contrast,eq=saturation=1.15:gamma=1.05"
```

O LUT real con `lut3d=file.cube`.

## Concatenación de segmentos

Después de componer cada segmento, el Music Mixer concatena todos. El filtro `concat` exige que todos los inputs tengan los mismos parámetros (timebase, pixfmt, sample rate). Para evitar sorpresas: re-encode cada segmento a un master template antes de concatenar (resolución fija, fps fijo, sample rate fijo).

```bash
ffmpeg -i seg1.mp4 -i seg2.mp4 -i seg3.mp4 \
  -filter_complex "[0:v][0:a][1:v][1:a][2:v][2:a]concat=n=3:v=1:a=1[v][a]" \
  -map "[v]" -map "[a]" -c:v libx264 -preset slow -crf 18 -c:a aac out.mp4
```

## Mezcla de música

Tres ingredientes:
- Audio del juego del clip compuesto (kills sounds, voice chat si está activado).
- Pista de música seleccionada.
- Ducking automático en momentos clave (flashbang, kill sound) para que se sienta el evento.

```
[0:a]asplit=2[a_game][a_game_sc];
[1:a]asplit=2[a_music][a_music_sc];
[a_music][a_game_sc]sidechaincompress=threshold=0.05:ratio=20:attack=5:release=300[a_music_ducked];
[a_game][a_music_ducked]amix=inputs=2:weights="1 0.6"[a_out]
```

La música baja automáticamente cuando suena algo fuerte en el juego, y vuelve. Sin trabajo manual.

## Wrapper: `ffmpeg-python` vs subprocess crudo

Trade-off:
- `ffmpeg-python` permite construir el filtergraph como AST → testeable, refactorable.
- Subprocess + strings es más cercano a los ejemplos de la doc, fácil de copiar y pegar.

Para zackvideo Composer y Mixer: `ffmpeg-python`. Los filtergraphs son largos y se generan dinámicamente según las reglas Lua; tener un AST evita errores de quoting / escapes.
