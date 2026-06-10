# Nexus Clips public recon notes

Date: 2026-06-03

Scope: public, unauthenticated research only. The crawl did not use accounts,
tokens, private endpoints, bypasses, or payloads against Nexus Clips. Raw dumps
were kept outside the repository under:

`C:\Users\reche\AppData\Local\Temp\zv-firecrawl-nexus-20260603-151912`

## Collection

Firecrawl CLI was used for public page discovery and content snapshots:

- `map https://nexusclips.com --limit 80 --sitemap include`
- `scrape https://nexusclips.com/es`
- `scrape https://nexusclips.com/es/streamers`
- `scrape https://nexusclips.com/es/clip-gaming`

The public Next.js build was then inspected locally from assets referenced by
the scraped page. Nexus currently exposes many `.js.map` files even though the
bundles do not advertise `sourceMappingURL` comments.

Observed build:

- Next build id: `yO9TLKjIplsX0HjiXtSyX`
- Public sourcemaps found: 57
- Public source entries indexed: 2363 total, including vendor/runtime noise
- High-signal app/package entries: 348 `src/` files plus internal package
  bundles such as `@nexusclips/core`, `@nexusclips/types`, and
  `@nexusclips/recording`

`studio.nexusclips.com` appeared in public assets, but Firecrawl could not
resolve DNS for that host during this run.

## Product Model

Nexus is structured around a generic video-to-short workflow rather than a
game-specific capture pipeline:

- Import sources: YouTube, Twitch, local uploads, standalone clips.
- Analyze long videos/streams into markers/moments.
- Let users select, trim, and edit a clip.
- Apply editor layers: subtitles, hook text, stickers, framing, watermark.
- Export vertical/horizontal variants.
- Publish or schedule to TikTok and YouTube Shorts.
- Track subscription, connected channels, notification preferences, and a
  publication calendar.

The public locale files and source traces repeatedly expose these namespaces:

- `videos`
- `video_import`
- `clip_import`
- `clip_create`
- `clip_edit`
- `clip_list`
- `editor`
- `markers`
- `publication`
- `calendar`
- `connection`
- `account_subscription`
- `account_connections`
- `account_notifications`
- `download`

## Technical Traces

Frontend architecture:

- Next.js on Vercel.
- Chakra UI based component system.
- Public app routes under `/app/...`, `/standalone/...`, `/uploaded/...`,
  `/twitch/...`, and marketing landing pages.
- Internal monorepo packages are bundled into the frontend:
  `@nexusclips/core`, `@nexusclips/types`, `@nexusclips/analytics`,
  `@nexusclips/logger`, `@nexusclips/nps`, and `@nexusclips/recording`.

Backend/API surface visible from public client code:

- API base: `api-prod.nexusclips.com`
- CDN hosts: `cdn.nexusclips.com`, `cdng.nexusclips.com`
- Storage traces: S3/Wasabi-style multipart upload flow
- OAuth/connectors: Google/YouTube, Twitch, TikTok
- Payments/analytics: Stripe, Sentry, Mixpanel, Statsig, Hotjar, Clarity

Endpoint categories visible from the bundled client include:

- Auth/session: auth token and logout.
- User/account: user profile, preferences, notifications.
- Connections: channel preferences and platform revoke flows.
- Video import/process: YouTube, Twitch, uploaded, standalone.
- Clip generation: unified clips, vertical clips, imported clips, manual clips.
- Markers/moments: Twitch stream moments and pending markers.
- Editor/export: render tasks, render elements, record presigned URLs.
- Uploads: multipart initiate, parts, complete, abort.
- Publication: calendar clips, share info, TikTok direct-post info.
- Templates: editor templates and recorded template assets.
- Subscription: active/cancel/manage flows.

Useful state/status names:

- `PROCESSING_UPLOAD`
- `PROCESSING_TRANSCRIPTION`
- `PROCESSING_MARKERS`
- `PROCESSING_EDITION`
- `PROCESSING`
- `REPROCESSING`
- `COMPLETED`
- `FAILED`
- `PUBLISHED`
- `SCHEDULED`
- `PRIVATE`
- `VIDEO_WITHOUT_AUDIO`
- `TRANSCRIPTION_EMPTY`
- `NO_MARKERS_GENERATED`

Clip/source type names:

- `YOUTUBE_AI_CLIP`
- `TWITCH_CLIP`
- `TWITCH_CLIP_FROM_STREAM`
- `STANDALONE_CLIP`
- `UPLOADED_CLIP`
- `YOUTUBE_SHORTS`
- `TIKTOK`

## What To Borrow

For ZackVideo, the strongest ideas are product and workflow contracts, not
implementation details.

1. Add a first-class `Moment` layer.

   Today ZackVideo has strong kill/utility segment planning. Nexus suggests a
   more generic layer above raw segments: a ranked moment with source, time
   span, score, reason codes, transcript/context, and render readiness.

2. Separate analysis from clip creation.

   Nexus treats video processing/marker generation as an async stage, then clip
   creation as a separate action. For ZackVideo this maps cleanly to parser
   output, scoring, manual review, and render task creation.

3. Model editor configuration as data.

   Their editor centers on templates plus layers: subtitles, hook, sticker,
   framing, watermark, shots. ZackVideo can keep FFmpeg/HLAE deterministic but
   store a portable edit document for each render variant.

4. Track render/export tasks explicitly.

   Nexus has render task and record/export traces. ZackVideo already gained a
   `render:variant` task type in the Allstar-inspired iteration; the next step
   is to connect it to actual render state and artifacts.

5. Build a local publication calendar.

   The useful idea is not cloud publishing, it is a local calendar/status board:
   draft, ready, scheduled, published, failed, needs caption, needs cover.

6. Use Codex CLI as control plane, not frame processor.

   Codex CLI can orchestrate deterministic local tools: Go parser, FFmpeg,
   Whisper/faster-whisper, OpenCV/scene detection, subtitle generation, cover
   selection, and metadata writing. The heavy work should remain in local,
   repeatable binaries and Go workers.

## ZackVideo Candidate Slices

Near-term slices that fit the current architecture:

- `internal/moments`: normalized moment type with score, source segment,
  reason codes, and selected render preset.
- `internal/moments/scoring`: initial deterministic scoring from kills,
  multikills, clutch context, smoke utility, round importance, and POV quality.
- `internal/editor/template`: JSON edit document with layers for subtitles,
  hook, sticker, framing, cover, and output aspect.
- `internal/tasks`: worker payloads for moment analysis and render variant
  execution.
- `internal/artifacts`: stable keys for moment JSON, edit documents, generated
  subtitles, covers, and final publish packs.
- `internal/httpapi`: local endpoints for moment list, moment detail, mark as
  selected, render variant, and publish status.
- `overlays/` or a lightweight local UI: marker review board inspired by Nexus
  `videos -> markers -> clip_edit -> publication`, but CS2-specific.

## Security Notes

The exposed sourcemaps are a defensive lesson: do not ship production source
maps for ZackVideo cloud/UI builds unless they are intentionally public. If we
need production diagnostics, upload sourcemaps privately to the error tracker
and keep the static `.map` files off the public CDN.

Do not rely on frontend-only checks for auth, plans, or quotas. Nexus exposes
many client-visible states and routes, but the important controls must live on
the backend. ZackVideo's local-first model reduces this risk, but any future
HTTP/API surface should still treat the frontend as untrusted.
