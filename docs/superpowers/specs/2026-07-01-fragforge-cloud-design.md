# FragForge Cloud - Diseño (subproducto TypeScript sobre Vercel)

> **Nota (2026-07-08):** el plano de datos descrito aquí (Supabase Storage para demos/artefactos, `jobs` en Postgres, `claim_next_job`) fue rediseñado local-first.
> Ese rediseño reemplaza las secciones 7 (Storage) y 9 (URLs firmadas) de este documento.
> Ver `docs/superpowers/specs/2026-07-08-local-first-cloud-data-plane.md` para el contrato vigente.
> Este documento se conserva sin reescribir como registro histórico del brainstorming original.

- Fecha: 2026-07-01
- Estado: aprobado (brainstorming), pendiente de plan de implementación
- Autor: reche + Claude

## 1. Contexto y motivación

FragForge existe hoy como una CLI y un orchestrator en Go que corren localmente en Windows.
El objetivo de este diseño es extraer un subproducto separado de la CLI, desplegable como producto web, manteniendo el flujo que ya refleja la web actual.
El flujo deseado es: subir una demo, ver la lista de jugadores, elegir un jugador, elegir jugadas específicas en un selector, y al elegir una o más, ejecutar la generación del reel, momento en el que se abren HLAE+CS2 para capturar los frames y luego se aplica la edición correspondiente.

La ambición inicial era "todo end-to-end en TypeScript, desplegado en Vercel".
El brainstorming reveló una frontera física que redefine el planteamiento y que este documento deja explícita.

## 2. La frontera física (restricción que define todo)

La captura de gameplay usa HLAE + CS2.
Eso requiere obligatoriamente Windows, una GPU, CS2 instalado, HLAE instalado, y el juego renderizando de verdad.
Vercel es Linux serverless, sin GPU, sin el juego, y sin procesos que duren minutos.
Por tanto, código que corra en Vercel no puede abrir HLAE+CS2 en ninguna máquina.
Esta limitación es de plataforma y de hardware, no de lenguaje.

La consecuencia es que la captura solo puede ocurrir en una máquina que tenga la GPU y el juego, y esa máquina no es Vercel.
La frase "en ese momento se abre HLAE+CS2" solo la puede cumplir un proceso que corra en el PC del usuario.

## 3. Decisiones tomadas en el brainstorming

Estas decisiones son la base del diseño y no deben re-litigarse sin motivo nuevo.

1. **Dónde ocurre la captura:** en el PC Windows del usuario, con Vercel como cerebro (control plane).
   Descartadas: GPU en la nube como SaaS (coste e infierno de Steam/VAC/licencias) y producto solo-análisis sin vídeo (contradice el flujo pedido).
2. **Lenguaje del agente del PC:** se reutiliza el worker Go actual como agente.
   El binario que instala el usuario es Go; solo se le añade el transporte hacia la nube.
   Se descartó reescribir el pipeline a TypeScript por la regresión de calidad en el parseo determinista y el coste de sustituir `demoinfocs-golang`.
3. **Dónde se parsea la demo:** en el agente (PC del usuario).
   Vercel no ejecuta ningún binario nativo; es un coordinador puro TS + UI.
   Se acepta como contrapartida que el usuario necesita su PC emparejado y encendido para ver el roster, lo cual ya es necesario para capturar de todos modos.
4. **Plano de datos gestionado:** Supabase (Postgres + Storage + Realtime).
   Consolida estado, blobs y progreso en vivo en un proveedor gestionado, usa el mismo motor Postgres que el orchestrator actual, y Realtime evita construir una capa de polling.
   El login Steam se queda en el Next.js.
5. **Transporte agente-nube:** el agente conecta hacia fuera (long-poll saliente).
   Nunca recibe conexiones entrantes, así que atraviesa NAT sin abrir puertos en el PC.

## 4. Arquitectura: tres planos, una frontera dura

```text
┌──────────────────── VERCEL (TypeScript, sin código nativo) ─────────────────────┐
│  FragForge Cloud (Next.js App Router)                                            │
│   · Web UI: subir -> roster -> jugador -> selector multi-jugada -> "Generar reel"│
│   · API de control (route handlers): jobs, pairings, URLs firmadas               │
│   · Auth Steam (ya existe) + sesión                                              │
└───────────────┬──────────────────────────────────────────────────┬─────────────┘
                │ (Postgres/Storage/Realtime)                        │ HTTP saliente
                ▼                                                    ▲ (long-poll)
     ┌─────────────────────┐                          ┌──────────────┴───────────────┐
     │  SUPABASE           │◀── demo / reel blobs ────▶│  zv-agent (Go, PC Windows+GPU)│
     │  · Postgres (estado)│                          │   · long-poll: reclama job     │
     │  · Storage (blobs)  │                          │   · REUSA parse/scan (parser)  │
     │  · Realtime (push)  │                          │   · REUSA record (HLAE+CS2)    │
     └─────────────────────┘                          │   · REUSA compose (FFmpeg/Lua) │
                                                       │   · sube reel + progreso       │
                                                       └────────────────────────────────┘
```

La frontera es estricta.
Vercel nunca ejecuta binarios nativos ni toca la GPU.
El agente nunca recibe conexiones entrantes; conecta hacia fuera.
Supabase es el único estado compartido entre ambos planos.

Todo vive en el mismo repositorio (monorepo).
Los binarios Go son el núcleo compartido: la CLI local (`zv short`, `zv serve`) sigue existiendo intacta para usuarios locales, y el producto-nube reutiliza los mismos paquetes `internal/*`.
Hay un solo origen de verdad para el parseo determinista y la captura.

## 5. Componentes

### A) FragForge Cloud (Vercel / TypeScript)

Es lo único desplegado en Vercel.
Contiene los route handlers del control-plane (crear job, reclamar/actualizar job, emitir URLs firmadas de subida/bajada), la UI, y la auth Steam.
Usa el cliente Supabase server-side.
No contiene FFmpeg, ni parser, ni HLAE.

### B) Supabase

Postgres guarda el estado (usuarios, agentes emparejados, jobs, selecciones de momentos, artefactos).
Storage guarda los blobs (demos que entran, reels + covers + manifests que salen).
Realtime empuja el estado del job a la UI en vivo.
Se elige por consolidar los tres servicios en un proveedor gestionado, por usar el mismo motor Postgres que ya se usa, y porque Realtime da el progreso en vivo sin construir polling.

### C) zv-agent (Go, PC del usuario)

Binario nuevo `cmd/zv-agent`.
Lo único nuevo de Go es fino y aislado:

- Un bucle de long-poll que reemplaza al consumidor Asynq: reclama job, despacha al core op correcto, reporta estado, repite.
- Un `JobRepository` respaldado por la API de la nube en vez de Postgres.
- Un `storage.Storage` respaldado por URLs firmadas de Supabase en vez de disco local.

Reutiliza tal cual la autodetección de HLAE/CS2/FFmpeg y los métodos core `parse` / `scanRoster` / `record` / `compose` de `internal/workers`, `internal/parser`, `internal/recording` y `internal/editor`.
El acoplamiento actual a Asynq es superficial: cada handler (`HandleParseDemo`, `HandleRecordDemo`, `HandleComposeFinal`) es una fina envoltura sobre un método core que solo depende de `job.Job` + `JobRepository` + `storage.Storage`, así que la lógica pesada se reusa casi verbatim.

## 6. Flujo end-to-end

1. **Login y emparejar PC (una vez):** login Steam.
   La nube muestra un código de emparejamiento; el usuario ejecuta `zv-agent --pair <código>`.
   El agente guarda un token con scope y empieza a latir (heartbeat, que lo marca "online").
2. **Subir demo:** el navegador pide una URL de subida firmada y sube la `.dem` directo a Supabase Storage (resumable para ficheros grandes).
   La nube crea un `job` (`queued`, tipo `parse`).
3. **Parseo (en el agente):** el agente reclama el parse job, descarga la demo, ejecuta `scanRoster`/`parse`, sube roster + momentos, y el job pasa a `parsed`.
   Realtime actualiza la UI.
4. **Selección:** la UI muestra el roster, eliges jugador, y aparece el selector multi-jugada (alimentado por el JSON de momentos).
   Marcas una o más jugadas más preset/opciones y pulsas "Generar reel".
5. **Job de captura:** la nube crea un `job` de captura con los segment IDs elegidos más la configuración de render.
6. **Captura y composición (en el agente):** el agente reclama el job y aquí se abren HLAE+CS2 y captura los segmentos elegidos.
   `compose` concatena el reel vertical con FFmpeg/Lua, sube reel + cover + caption/manifest, y el job pasa a `done`.
   Realtime empuja progreso en cada subpaso (grabando X/N, componiendo, subiendo).
7. **Entrega:** la UI reproduce el reel desde una URL firmada y presenta el pack listo para subir, respetando la convención `shortslistosparasubir`.

## 7. Modelo de datos

Postgres (Supabase), tablas mínimas:

- `users(id, steam_id unique, persona, avatar, created_at)` es la identidad Steam.
- `agents(id, user_id, name, token_hash, status, last_heartbeat_at, capabilities jsonb, created_at)` son los PCs emparejados; `capabilities` guarda la autodetección HLAE/CS2/FFmpeg que ya existe.
- `demos(id, user_id, storage_key, filename, size, sha256, roster jsonb, parse_key, state, created_at)` guarda un `roster` compacto en DB para listar rápido; el resultado completo de parseo (momentos) vive como artefacto JSON en Storage referenciado por `parse_key`.
- `jobs(id, user_id, agent_id, demo_id, type[parse|capture], state, payload jsonb, result jsonb, attempt, lease_expires_at, error, created_at, updated_at)` es la máquina de estados, adaptada de la actual.
- `reels(id, demo_id, user_id, player_steam_id, segment_ids text[], preset, edit_config jsonb, state, output_keys jsonb, created_at)` es la entidad de producto del job de captura.

## 8. Contrato de API de control

Route handlers Next.js (TS), con dos audiencias y auth distinta.

Navegador (cookie de sesión):

- `POST /api/demos` crea la demo y devuelve una URL de subida firmada.
- `GET /api/demos/:id` devuelve estado, roster y momentos.
- `POST /api/reels` crea el job de captura con `{demoId, playerSteamId, segmentIds[], preset, editConfig}`.
- `GET /api/reels/:id` devuelve estado y URLs de salida.
- `GET /api/agents` lista agentes emparejados y su estado online.
- `POST /api/agents/pair` emite un código de emparejamiento.

Agente (bearer token, namespace `/api/agent/*`):

- `POST /pair` canjea el código por un token.
- `POST /heartbeat` reporta online más capabilities.
- `POST /jobs/claim` hace long-poll: reclama atómicamente el siguiente job de SU usuario con un lease, o devuelve 204 al expirar el timeout.
- `POST /jobs/:id/status` reporta progreso (estado, porcentaje, mensaje) y renueva el lease.
- `POST /jobs/:id/complete` y `POST /jobs/:id/fail` cierran el job con las claves de resultado o el error.
- `GET /blobs/:key/download` devuelve una URL firmada de bajada de la demo.

Claim atómico: `SELECT ... FOR UPDATE SKIP LOCKED` toma el job `queued` más antiguo del usuario del token, lo marca `claimed` con `lease_expires_at = now()+N`.
El status renueva el lease.
Un reaper re-encola parse jobs cuyo lease expiró (el agente murió); los de captura van a `failed` porque no se auto-reintenta una captura con GPU, exactamente la política actual.

## 9. Storage

Buckets privados en Supabase Storage:

- `demos/{userId}/{demoId}.dem`
- `artifacts/{demoId}/parse.json`
- `reels/{reelId}/final.mp4`, `.../cover.jpg`, `.../caption.txt`, `.../manifest.json`

Todo es privado.
El acceso es siempre vía URLs firmadas de vida corta (5-15 min) que emite el control-plane.
El agente nunca tiene credenciales globales de Storage.
Las demos grandes usan subida resumable (TUS) para robustez.

## 10. Seguridad y emparejamiento

- Login: Steam OpenID (ya existe) más cookie de sesión.
- Emparejamiento: el usuario autenticado pulsa "Pair PC" y la nube genera un código corto (TTL 10 min, un solo uso).
  El usuario ejecuta `zv-agent --pair <código>`, el agente lo canjea, la nube crea la fila `agents` y devuelve un token de agente (256-bit aleatorio, guardado hasheado en DB, mostrado una vez, escrito en el config del agente con permisos 0600).
  Todas las llamadas `/api/agent/*` usan `Authorization: Bearer <token>`.
- Aislamiento multi-tenant: el token de un agente solo autoriza los jobs de su usuario; el claim filtra por `user_id`.
  Un agente jamás ve demos ni jobs de otro usuario.
- URLs firmadas: demos y reels son privados; el control-plane las emite bajo demanda.
  La service key de Supabase vive solo server-side en Vercel, nunca en el navegador ni en el agente.
- Sin puertos abiertos: el agente solo conecta hacia fuera, atraviesa NAT sin exponer nada en el PC.
  HLAE/CS2 se lanzan en modo ventana (regla operativa del repo).
- Anti-abuso: validación de `.dem` (tope de tamaño más cabecera mágica, reusando la validación de subida actual) y rate-limit de subidas y de creación de jobs por usuario.

## 11. Errores e idempotencia

- Idempotencia heredada: cada core op comprueba si el artefacto durable ya existe en Storage y se salta el comando de media si sí, de modo que los reintentos manuales son seguros.
- Política de reintento heredada: parse auto-reintenta por ser etapa pura; captura no se auto-reintenta porque cuesta minutos de GPU y pasa a `failed` para que el usuario decida.
- Agente offline: si ningún agente late, los jobs quedan `queued` y la UI indica "tu PC está desconectado, abre FragForge Agent".
  Realtime lo voltea al volver el heartbeat.
- Fallos de transporte: long-poll con timeout más backoff exponencial; los `complete` duplicados se tratan idempotentes (si ya está `done`, se responde ok).
- Doble registro de errores: el agente usa `internal/obs` local (journal más métricas en el PC) y además reporta el fallo terminal a la nube (`jobs.error`) para que la UI muestre un mensaje accionable, por ejemplo "captura no configurada: no se encontró HLAE".

## 12. Testing

- Nube (TS): tests de los route handlers (creación de job, claim, emisión de URLs firmadas) y la e2e de Playwright existente para el flujo UI, adaptada al nuevo control-plane conservando el contrato `503 service_unavailable`.
- Agente (Go): se reutilizan los tests de workers actuales.
  Los adaptadores nuevos llevan table tests: el `JobRepository` HTTP (claim/status/complete contra un server falso), el `storage.Storage` de Supabase (subida/bajada firmada contra un fake), y el bucle long-poll (reclama, despacha, reporta, con renovación de lease y backoff).
  No se usa HLAE/CS2 real en tests.
- Contrato compartido: golden JSON del contrato de la API para que el agente Go y la nube TS no diverjan.
- Modo memoria/local: como el `ZV_DATABASE_URL=memory` de hoy, un modo fake-cloud para desarrollar el agente sin Supabase.

## 13. Alcance, no-objetivos y fases

No-objetivos de este diseño:

- No hay captura en la nube; la GPU siempre es la del usuario.
- No se reescribe el pipeline Go a TypeScript.
- No se rompe la CLI local existente; sigue funcionando igual.

Delta de producto identificado:

- El selector de jugadas hoy es de selección única (`selectedPlayId` en `web/app/(app)/matches/[id]/page.tsx`).
  El flujo pedido requiere multi-selección (elegir una o más jugadas para un reel concatenado), alineado con el default de CLAUDE.md de "un Short largo con todas las kills seleccionadas".
  Es una feature nueva a construir.

Fases sugeridas (a detallar en el plan de implementación):

- Fase 1: control-plane más esquema Supabase más esqueleto del agente (pair, heartbeat, claim, parse job).
  Meta: subir demo y ver roster funciona end-to-end.
  Puede arrancar con un único agente emparejado (subconjunto del modelo multi-tenant, mismo mecanismo de token).
- Fase 2: job de captura (record más compose) end-to-end, con el reel entregado en la UI.
- Fase 3: selector multi-jugada pulido, presentación del pack de publicación, subidas resumables, y endurecimiento de leases/reaper.
