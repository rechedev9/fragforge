# Web image: builds the Next.js app as a standalone server and runs it.
# NEXT_PUBLIC_API_BASE is baked at build time so the client uses the real API
# client (same-origin /api proxy); ORCHESTRATOR_URL/ORCHESTRATOR_TOKEN are read
# at runtime (set by compose) to reach the orchestrator container.
# syntax=docker/dockerfile:1

FROM node:22-slim AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22-slim AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# public/ only holds gitignored media locally (sample-reel.mp4), so a fresh
# clone has no such directory at all; guarantee it exists so the runner's COPY
# never fails on a clean checkout.
RUN mkdir -p public
# local mode: the compose stack is the self-contained no-login analyze flow
# (web -> orchestrator container), not the Supabase cloud control-plane. The
# var is inlined into the client bundle here and must also be present at
# runtime for the server route handlers (set in docker-compose.app.yml).
ENV NEXT_PUBLIC_API_BASE=/api \
    NEXT_PUBLIC_FRAGFORGE_MODE=local \
    NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:22-slim AS runner
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0
# The standalone output bundles only the server's runtime deps.
COPY --from=build /app/public ./public
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
EXPOSE 3000
CMD ["node", "server.js"]
