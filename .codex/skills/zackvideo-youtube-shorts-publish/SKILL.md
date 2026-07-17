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
<run>\shortslistosparasubir\
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
.\bin\zv.exe workflows run gallery-open -- --path <run>\shortslistosparasubir\index.html
```

## Publish Approval Gate

Before presenting a pack as upload-ready, ask the user only for the publish choices they have not already supplied, grouped into one concise message:

- final title wording per video, offering a concrete suggestion in the pattern below;
- caption text and hashtag set, offering a concrete suggestion;
- thumbnail: show the cover sheet or candidate images and ask the user to choose one, or confirm no cover;
- language of titles/captions when the audience is not obvious from the request.

If the user delegates these choices, state the resolved defaults and continue.
Do not call the pack upload-ready until every question is answered or explicitly delegated, and never repeat a question the request already answered.

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
