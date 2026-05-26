---
name: zackvideo-youtube-shorts-publish
description: "Prepare or upload ZackVideo YouTube Shorts publish packs with titles, captions, hashtags, covers, and optional YouTube Data API upload."
---

# ZackVideo YouTube Shorts Publish

Use this skill when the user asks for YouTube titles, captions, Shorts upload packs, or uploads.

## Publish Pack

ZackVideo writes publish assets under:

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

## Upload Notes

YouTube upload should use the YouTube Data API `videos.insert` endpoint with OAuth. Do not promise public visibility before checking the account/API status: uploads may default to private or be restricted by channel verification, API quota, or policy checks.

Before uploading, confirm the exact account/channel and whether the user wants `private`, `unlisted`, or `public`.
