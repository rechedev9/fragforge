# Research — Prior art y aprendizajes

Proyectos y productos del espacio "CS2 highlight automation". Lo que copiamos, lo que no.

## CS2-Highlight-Automator-for-OBS (Keanoski)
https://github.com/Keanoski/CS2-Highlight-Automator-for-OBS

**Qué hace:** parsea `.dem` con `DemoFile.Game.Cs` (C#), detecta kill streaks (default ≥5 kills), lanza CS2 con configs auto-generados, controla OBS por WebSocket para grabar.

**Stack:** C# (.NET 6+), DemoFile.Game.Cs, obs-websocket-dotnet. Docker incluido.

**Estado:** WIP, sin releases.

**Qué tomamos:**
- Confirma que el patrón "parse demo → generar config + cfg de CS → grabar" es viable.
- Idea de auto-generar config con `demo_gototick start/end` + cfgs cinemáticos.

**Qué no tomamos:**
- C# no es nuestro stack. Si necesitamos un parser, ya elegimos demoinfocs-golang.
- OBS WebSocket lo descartamos para V1 (ver `03-recording-options.md`).
- La heurística "≥5 kills" es muy restrictiva — perdemos kills individuales con AWP que sí son virales. Nuestras reglas son configurables por preset.

## CS Demo Manager
https://cs-demo-manager.com

**Qué hace:** producto cliente desktop para analizar / visualizar / editar demos de CS2. Tiene módulo de generación de vídeo basado en HLAE.

**Stack:** Electron + Node + binarios externos.

**Aprendizajes:**
- Validan que HLAE puede orquestarse desde un programa externo de forma productiva.
- Su módulo "video" genera scripts HLAE y lanza CS2 + HLAE programáticamente — exactamente el patrón que proponemos en el Recording Driver.
- Decisión de usar Source 2 commands (`mirv_streams` etc.) en vez de OBS es alineada con la nuestra.

## LHM.gg (Auto Replay Generator)
https://lhm.gg/features/auto-replay-generator

**Qué hace:** producto comercial SaaS para auto-generar replays de CS2 y Dota 2. Probablemente integra con cuentas para descarga automática de demos de Faceit.

**Aprendizajes:**
- Confirma que hay mercado para esto y que la forma "SaaS web" funciona.
- Integración con Faceit API para descarga automática de demos es un add-on interesante para V2 (en V1 el usuario sube el `.dem` a mano).

## Eklipse
https://eklipse.gg/use-case/counter-strike-2-highlights/

**Qué hace:** AI clip generator multi-juego (Twitch / OBS captures). Es más "highlight detection sobre vídeo grabado", no toca demos.

**Aprendizajes:**
- Stack ML sobre vídeo → caro y complicado, y no resuelve la fase de grabación.
- Confirma que el approach demo-first (información estructurada de qué pasó) es superior al video-first para gaming.

## cs-demo-analyzer (akiver)
https://github.com/akiver/cs-demo-analyzer

**Qué hace:** CLI Go que analiza demos y emite JSON con stats. Usa internamente demoinfocs-golang.

**Aprendizajes:**
- Muestra el patrón limpio "demo → JSON" en Go.
- Su estructura de output puede inspirar el formato de nuestro `kill plan`.

## LaihoE/demoparser
https://github.com/LaihoE/demoparser

**Qué hace:** parser Rust con bindings Python/JS, muy rápido.

**Por qué no es nuestro elegido:** el usuario prefiere demoinfocs-golang explícitamente. demoparser es valioso si en algún momento necesitamos parsear miles de demos para análisis bulk (no es el caso en V1).

## Síntesis

Nadie ofrece exactamente lo que zackvideo se propone (demo → grabación cinemática + edit con efectos + música sincronizada, todo automático, listo para redes). Los más cercanos son:
- **CS Demo Manager + Eklipse** → cubren parte (analyzer + clip detector) pero ninguno los dos.
- **LHM.gg** → SaaS comercial que probablemente sí lo hace, pero es competencia/benchmark, no referencia open source.

Diferencial de zackvideo:
1. Demo-first (estructurado, no heurístico).
2. Calidad de grabación de HLAE (no capture screen).
3. Reglas de efectos editables en Lua (no caja negra).
4. Música sincronizada al beat con librosa.
