FragForge local toolchain diagnosis. Optional focus: $ARGUMENTS

Read-only diagnosis. Do not install tools or edit files unless the user asks.

Goal: diagnose whether the local machine is ready for FragForge development and
capture/edit workflows.

Process:

1. Run `git status --short`.
2. Inspect `docs/toolchain.md` and relevant scripts.
3. Run safe diagnostics only:
   - `scripts/go-tools-check.sh`
   - if on Windows/WSL and available, run the PowerShell toolchain check in a
     read-only way, for example:
     `powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/check-toolchain.ps1`
   - use `-StrictCapture` only if the user is diagnosing real capture paths.
4. Check Go, goimports, staticcheck, govulncheck, gosec, Docker, FFmpeg,
   ffprobe, Node/npx, HyperFrames, HLAE, CS2, and HLAE FFmpeg config as relevant.
5. Do not run CS2/HLAE, Docker compose, migrations, or renders.
6. Summarize missing tools, exact install commands, and whether each missing
   piece blocks parser-only work, media editing, orchestration, or real capture.

Output:

- Ready now
- Missing optional tools
- Missing required tools
- Capture-specific issues
- Exact next commands
