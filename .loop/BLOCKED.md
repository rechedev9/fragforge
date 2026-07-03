# BLOCKED

Cola de bloqueos: solo Luis borra entradas; el loop nunca reintenta una acción listada aquí.
Antes de añadir, comprobar si ya existe una entrada para el mismo bloqueo y actualizarla en vez de duplicar.

<!-- Formato de entrada (ejemplo comentado):
## B1 - vercel login requiere aprobación humana
Intentado: `vercel whoami` (2026-07-03) -> arrancó device flow y quedó esperando.
Comando exacto a reproducir: `vercel login` y visitar la URL de device que imprime.
Qué falló: no hay sesión de Vercel en esta máquina y el device code necesita un humano.
Desbloqueo (< 1 min): Luis escribe `! vercel login` en la sesión y aprueba en el navegador.
-->

## B1 - Root Directory del proyecto Vercel no se puede fijar a "landing" desde la CLI
Intentado: `vercel link --yes --project fragforge-landing` desde landing/ + `vercel project inspect fragforge-landing` (2026-07-03) -> `Root Directory: .`.
Qué falla: la CLI de Vercel no tiene comando para cambiar rootDirectory, y usar la API REST exigiría leer el token local (prohibido por PLAN).
Impacto real hoy: ninguno; el proyecto no tiene integración git, y los deploys por CLI suben solo el contenido de landing/, así que web/ no puede desplegarse.
Riesgo futuro: si algún día conectas el repo GitHub al proyecto en el dashboard, construiría la raíz del monorepo.
Desbloqueo (< 1 min): en el dashboard de Vercel, fragforge-landing -> Settings -> Build & Deployment -> Root Directory = `landing`. Hazlo antes de (o junto con) cualquier git-connect.
