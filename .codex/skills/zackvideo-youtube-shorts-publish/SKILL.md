---
name: zackvideo-youtube-shorts-publish
description: "Prepare FragForge YouTube Shorts publish packs with titles, captions, hashtags, covers, and manual YouTube Studio guidance. Use when Codex needs to create or review upload-ready metadata and guide a user through the official browser-based publication flow."
---

# FragForge YouTube Shorts Publish

Prepare and review upload-ready YouTube Shorts assets. Keep account, audience,
visibility, scheduling, and publication decisions inside YouTube Studio.

## Publish Pack

FragForge writes publish assets under:

```text
<shorts>\publish\
```

Expected files:

- numbered `.mp4` Shorts
- matching `.caption.txt`
- `.cover.jpg` files when covers are enabled
- `pack-manifest.json`
- `publish-summary.md`
- `index.html`

Open the gallery for review:

```powershell
.\bin\zv.exe workflows run gallery-open -- --path <shorts>\publish\index.html
```

## Title/Captions

Use human-readable, search-relevant titles:

```text
iM T Ramp Smoke from CT Spawn | Inferno CS2
```

Caption pattern:

```text
iM throws a standing jumpthrow smoke on Inferno: CT Spawn to T Ramp.
CS2 Inferno utility reference.

#CS2 #CounterStrike2 #Inferno #Smoke #CS2Lineups
```

Keep hashtags relevant and limited. Do not spam broad tags.

## Manual publication

Download the finished MP4 and open only:

```text
https://studio.youtube.com/
```

Guide the user through YouTube Studio's official **CREAR -> Subir vídeos** flow.
Do not request Google credentials or attempt account connection or direct
publication. Leave the channel, made-for-kids declaration, visibility, and
schedule for the user to select in Studio. Follow YouTube's official guide:
https://support.google.com/youtube/answer/57407?hl=es
