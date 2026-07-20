# FragForge Studio 2.2.9 — incidencias de QA

Fecha de la prueba: 20 de julio de 2026  
Entorno: Windows 11, FragForge Studio 2.2.9  
Estado general: incidencias abiertas, salvo que se indique lo contrario.

## Alcance de la prueba

Se probaron los flujos reales de demos y clips de stream, incluyendo:

- Navegación general y asistente integrado.
- Selección de jugador, momentos y creación de reels desde demos.
- Importación de un clip real de Twitch.
- Recorte de facecam, rangos, killfeed, subtítulos y render vertical.
- Reproducción del resultado y preparación de los artefactos finales.

Clip real empleado:

- Canal: `zacketizorcs2`
- Título: `vaya saco..`
- URL: <https://www.twitch.tv/zacketizorcs2/clip/CulturedStormyKittenAllenHuhu-sMpul952n8rWF81N>
- Duración detectada: 15,112 s
- Job probado: `90ea8bb9-0d83-4fe6-b150-573196b46796`

## Resumen

| Severidad | Cantidad |
| --- | ---: |
| BLOCKER | 7 |
| WARNING | 11 |
| NIT | 5 |
| **Total** | **23** |

## Incidencias

### FF-001 — Se heredan metadatos de publicación de un reel anterior

**Severidad:** BLOCKER  
**Área:** Publicación / aislamiento entre proyectos

**Comportamiento observado:** al abrir las sugerencias de publicación de un reel nuevo aparecieron título, textos o recomendaciones pertenecientes a un reel anterior.

**Resultado esperado:** cada reel debe cargar únicamente sus propios metadatos. Si todavía no existen, los campos deben aparecer vacíos o generarse a partir del reel actual.

**Riesgo:** publicar un vídeo con información de otro cliente, jugador o partida.

### FF-002 — Se crea un reel de demo sin pasar por el brief creativo

**Severidad:** BLOCKER  
**Área:** Demo / creación de reel

**Pasos observados:**

1. Seleccionar una jugada de una demo.
2. Pulsar `Forjar reel`.

**Comportamiento observado:** el reel pasó directamente a `LISTO` sin pedir ni confirmar formato, HUD, killfeed, efectos, transiciones, contador, música, intro/outro o portada.

**Resultado esperado:** mostrar y aprobar el brief creativo completo antes de cualquier captura o render costoso.

### FF-003 — El asistente pierde el contexto de la sección actual

**Severidad:** WARNING  
**Área:** Asistente integrado

**Comportamiento observado:** desde `Noticias` o `Feed`, el asistente respondió como si el usuario estuviera en la pantalla principal de Studio.

**Resultado esperado:** el asistente debe recibir la ruta y el contexto funcional de la vista activa.

### FF-004 — Una URL de Twitch vacía no muestra validación

**Severidad:** WARNING  
**Área:** Importación de streams

**Pasos observados:** dejar vacío el campo de URL de Twitch e intentar continuar.

**Comportamiento observado:** no aparece un error visible ni una indicación accionable.

**Resultado esperado:** validación inmediata junto al campo, explicando que la URL es obligatoria y qué formatos se admiten.

### FF-005 — La numeración de navegación no coincide con la cabecera

**Severidad:** WARNING  
**Área:** Navegación / consistencia visual

**Comportamiento observado:** los números o pasos mostrados en la navegación lateral no coinciden con los de la cabecera de la pantalla.

**Resultado esperado:** una única secuencia y nomenclatura para todo el flujo.

### FF-006 — Aparecen filas de jugadas duplicadas

**Severidad:** NIT  
**Área:** Demo / listado de jugadas

**Comportamiento observado:** algunas jugadas se presentan más de una vez en el listado.

**Resultado esperado:** deduplicar por demo, ronda, tick y jugador, o explicar visualmente por qué son candidatos distintos.

### FF-007 — Ajustes no muestra la versión instalada

**Severidad:** NIT  
**Área:** Ajustes / soporte

**Comportamiento observado:** no se encontró la versión de FragForge dentro de `Ajustes`.

**Resultado esperado:** mostrar versión de aplicación, build y, si aplica, versión del motor local para facilitar soporte y reportes.

### FF-008 — Una demo histórica aparece etiquetada como “AHORA MISMO”

**Severidad:** WARNING  
**Área:** Demo / fechas

**Comportamiento observado:** una demo antigua se presentó con la etiqueta relativa `AHORA MISMO`.

**Resultado esperado:** calcular la antigüedad usando la fecha real del artefacto o mostrar una fecha absoluta cuando la procedencia temporal sea ambigua.

### FF-009 — Elegir un jugador inicia la forja sin un botón de continuación

**Severidad:** WARNING  
**Área:** Demo / selección de jugador

**Pasos observados:** pulsar un jugador de la plantilla.

**Comportamiento observado:** la interfaz comienza inmediatamente a `forjar highlights`.

**Resultado esperado:** la selección del jugador debe ser reversible y existir un botón explícito para continuar antes de iniciar trabajo adicional.

### FF-010 — Se repiten momentos al cargar una segunda demo

**Severidad:** NIT  
**Área:** Series / múltiples demos

**Comportamiento observado:** en la segunda demo aparecieron candidatos o momentos duplicados.

**Resultado esperado:** mantener identidad estable por demo y evitar mezclar o repetir candidatos entre partes de una serie.

### FF-011 — El botón flotante del asistente invade el CTA en anchuras intermedias

**Severidad:** NIT  
**Área:** Responsive / asistente

**Comportamiento observado:** alrededor de 960 px de ancho, el botón del asistente queda demasiado cerca o se solapa visualmente con la acción principal.

**Resultado esperado:** reservar espacio para ambos controles o recolocar el asistente según el breakpoint.

### FF-012 — El recorte automático de facecam selecciona el radar de CS2

**Severidad:** BLOCKER  
**Área:** Stream / composición vertical

**Pasos observados:**

1. Importar el clip real de Twitch.
2. Elegir el layout vertical con facecam y gameplay.
3. Revisar el recorte automático.

**Comportamiento observado:** la región propuesta como facecam apuntaba al radar de CS2, no a la cámara real del streamer.

**Resultado esperado:** detectar la cara o la región de cámara. Si la confianza es baja, pedir ajuste manual antes de renderizar.

### FF-013 — La previsualización anterior al render no reproduce el vídeo

**Severidad:** BLOCKER  
**Área:** Stream / previsualización

**Comportamiento observado:** el reproductor mostró `No se ha podido reproducir el contenido multimedia` y la previsualización quedó estática.

**Resultado esperado:** reproducir el montaje con los recortes y rangos actuales, o mostrar un error técnico accionable con opción de reintento.

### FF-014 — Navegar fuera del editor elimina el borrador del stream

**Severidad:** BLOCKER  
**Área:** Stream / persistencia

**Pasos observados:**

1. Importar el clip y configurar el montaje.
2. Navegar a otra sección.
3. Volver al editor de streams.

**Comportamiento observado:** la pantalla volvió al estado inicial, sin fuente ni opción para reanudar. Fue necesario importar otra vez y se creó un job nuevo.

**Resultado esperado:** persistir el plan de edición y ofrecer `Continuar borrador`, incluso después de navegar o reiniciar Studio.

### FF-015 — El detector de killfeed genera numerosos eventos duplicados

**Severidad:** BLOCKER  
**Área:** Stream / análisis de killfeed

**Comportamiento observado:** se detectaron 18 eventos en un clip de 15 segundos, con varios candidatos casi idénticos en `1,92`, `1,93`, `1,95` y `1,98` segundos.

**Resultado esperado:** agrupar detecciones de la misma fila mediante una ventana temporal y devolver un único evento por aparición real.

### FF-016 — El OCR del killfeed omite filas y corrompe nombres

**Severidad:** BLOCKER  
**Área:** Stream / OCR de killfeed

**Comportamiento observado:** en un fotograma con tres filas visibles, el análisis extrajo solo una. El atacante quedó como `#Ez4TGDZaCkETIZOR...CTT`, la víctima como `almazer1` y el arma como `ak47`.

**Resultado esperado:** extraer todas las filas visibles con nombres y arma correctos, o marcar el resultado como baja confianza para revisión manual.

**Riesgo:** generar overlays falsos o atribuir una baja al jugador equivocado.

### FF-017 — “Añadir rango” supera por defecto la duración del clip

**Severidad:** WARNING  
**Área:** Stream / rangos

**Comportamiento observado:** el rango inicial propuesto fue `0–20 s` aunque la fuente duraba `15,112 s`.

**Resultado esperado:** limitar automáticamente el final a la duración real del medio.

### FF-018 — La validación del rango aparece tarde y expone datos internos

**Severidad:** WARNING  
**Área:** Stream / validación

**Comportamiento observado:** el rango inválido no se señaló al crearlo; el error apareció al generar subtítulos, en inglés y mostrando el identificador interno del clip.

**Resultado esperado:** validación inmediata, localizada al español y sin IDs internos salvo en detalles técnicos desplegables.

### FF-019 — La generación de subtítulos devuelve `no_speech` en todo el clip

**Severidad:** WARNING  
**Área:** Stream / subtítulos / xAI  
**Estado:** requiere confirmación con más clips hablados.

**Comportamiento observado:** los dos rangos solapados devolvieron `no_speech`, aunque la fuente contenía audio prácticamente continuo.

**Resultado esperado:** distinguir entre ausencia real de voz, audio no analizado y fallo del proveedor. La UI debería permitir escuchar el tramo y reintentar.

### FF-020 — El análisis bloquea el formulario sin progreso ni ETA

**Severidad:** WARNING  
**Área:** Stream / análisis

**Comportamiento observado:** el formulario quedó bloqueado durante aproximadamente 22 segundos sin indicar fase, porcentaje o tiempo estimado.

**Resultado esperado:** mostrar la operación activa, progreso indeterminado o por fases, tiempo transcurrido y una cancelación segura.

### FF-021 — El layout de fotograma completo conserva controles propios de facecam

**Severidad:** WARNING  
**Área:** Stream / layouts

**Comportamiento observado:** al seleccionar el layout de fotograma completo permanecieron controles o textos relacionados con la separación y el recorte de facecam.

**Resultado esperado:** ocultar controles que no aplican al layout seleccionado y adaptar su descripción.

### FF-022 — El título del clip de Twitch no se rellena automáticamente

**Severidad:** NIT  
**Área:** Stream / metadatos

**Comportamiento observado:** aunque Twitch proporcionaba el título `vaya saco..`, el campo correspondiente no se completó.

**Resultado esperado:** importar título, canal y URL como metadatos editables, manteniendo siempre la atribución al origen.

### FF-023 — El render no genera portada ni paquete final de entrega

**Severidad:** WARNING  
**Área:** Stream / entrega

**Comportamiento observado:** el render terminó correctamente, pero Studio no creó la portada aprobada ni una carpeta final `shortslistosparasubir`. Fue necesario extraer un fotograma y reunir manualmente vídeo, portada, plan y manifiesto.

**Resultado esperado:** al finalizar, generar automáticamente el paquete acordado con:

- MP4 final.
- Portada seleccionada o generada.
- Plan de edición.
- Manifiesto del render.
- Subtítulos y metadatos cuando estén habilitados.

## Resultado técnico del render probado

- Vídeo: H.264, 1080×1920, 60 fps.
- Audio: AAC, 44,1 kHz, estéreo.
- Duración: 15,112 s.
- Tamaño: 14.388.280 bytes.
- Resultado visual: facecam superior y gameplay inferior tras corregir manualmente el recorte.
- HUD y killfeed originales conservados.
- Sin música, subtítulos ni killfeed sintético.

El render final se reprodujo correctamente. Las incidencias de esta lista afectan principalmente a la preparación, revisión, persistencia y empaquetado del flujo, no a la codificación final del MP4.
