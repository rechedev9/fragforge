ZackVideo generated artifact audit. Optional focus: $ARGUMENTS

Read-only. Do not edit or delete files.

Goal: identify generated media/demo/data artifacts that are accidentally present
in git status or tracked by git, and check whether `.gitignore` still protects
the repo.

Process:

1. Run `git status --short`.
2. Inspect `.gitignore`.
3. Check tracked files likely to be generated artifacts:
   - media: `.mp4`, `.mov`, `.webm`, `.avi`, `.mkv`, `.wav`, `.flac`, `.aac`
   - capture/render frames: `.tga`, `.exr`, `.y4m`, `.yuv`, `frames/`
   - demos/goldens: `.dem`, `.expected.json`
   - generated run data under `data/`
   - overlay build output under `overlays/**/dist`, `render`, `renders`, `out`
4. Check untracked suspicious artifacts, but do not recurse into huge trees more
   than needed. Prefer git-aware commands over broad filesystem scans.
5. Separate expected local artifacts from risky git additions.
6. Recommend exact cleanup or `.gitignore` changes, but do not apply them.

Output:

- BLOCKER: generated artifacts that appear tracked or likely to be committed.
- WARNING: suspicious untracked artifacts or ignore gaps.
- OK: protections that look correct.
- Suggested commands, if any, for the user to run manually.
