# FragForge security audit and triage

Audit date: 2026-07-23
Target: current `main` worktree, including the pre-existing uncommitted changes
Method: five independent read-only reviews in two waves, coordinator verification, focused tests, dependency audits, static analysis, and live release/repository posture checks

## Executive summary

The five reviewers returned 25 candidate findings. Correlating duplicate reports produced 19 unique findings:

| Severity | Count | Meaning in this report |
| --- | ---: | --- |
| `BLOCKER` | 2 | Fix before treating the local API or Electron approval gate as a security boundary. |
| `WARNING` | 13 | Concrete risk or material security debt; prioritize by the assigned `P1`/`P2`. |
| `NIT` | 4 | Defense in depth with limited standalone exploitability in the current local-first model. |

The highest-risk chain is:

1. the loopback Go API accepts an attacker-controlled `Host` under a same-origin DNS-rebinding request and has no token by default;
2. Electron exposes approval by action ID to the same renderer that displays untrusted application content;
3. the renderer/proxy can reach destructive and costly local operations.

The immediate containment work is therefore to authenticate every loopback API request with a per-session capability, validate the listener authority rather than trusting `Host`, and move human approval out of the content renderer.

## Finding index

| ID | Severity | Priority | Confidence | Surface | Triage |
| --- | --- | --- | --- | --- | --- |
| FF-SEC-001 | `BLOCKER` | P0 | High | Go HTTP API | Accepted: DNS rebinding bypasses the loopback trust model. |
| FF-SEC-002 | `BLOCKER` | P0 | High | Electron approval | Accepted: renderer code can approve privileged actions. |
| FF-SEC-003 | `WARNING` | P1 | High | VOD acquisition | Accepted: arbitrary HTTP(S) sources allow SSRF. |
| FF-SEC-004 | `WARNING` | P1 | High | HLAE script generation | Accepted: demo-controlled names inject console commands. |
| FF-SEC-005 | `WARNING` | P1 | High | Next proxy | Accepted and reproduced: token follows cross-origin redirects. |
| FF-SEC-006 | `WARNING` | P2 | High | Next proxy | Accepted: origin-less local clients inherit server authority. |
| FF-SEC-007 | `WARNING` | P1 | High | Stream persistence/API | Accepted: signed/source URLs are stored and returned raw. |
| FF-SEC-008 | `WARNING` | P1 | High | JS dependencies | Accepted as patch debt; current vulnerable features were not found reachable. |
| FF-SEC-009 | `WARNING` | P1 | High | Desktop release | Accepted: public installer has no authentic publisher signature. |
| FF-SEC-010 | `WARNING` | P2 | High | Non-loopback deployment | Accepted when an exposed bind is enabled: token and media use cleartext HTTP. |
| FF-SEC-011 | `WARNING` | P2 | Medium | Lua effects | Accepted for external scripts: CPU timeout does not bound memory/source/output. |
| FF-SEC-012 | `WARNING` | P2 | High | Lua/FFmpeg boundary | Accepted for external scripts: `image.path` permits filesystem/network inputs. |
| FF-SEC-013 | `WARNING` | P1 | High | Music bootstrap | Accepted: the shell downloader ignores catalog SHA-256 values. |
| FF-SEC-014 | `WARNING` | P1 | High | CI/repository governance | Accepted: vulnerable dependencies and secrets can reach unprotected `main`. |
| FF-SEC-015 | `WARNING` | P2 | High | Landing configuration | Accepted: `landing/.env*` is not ignored while secret scanning is disabled. |
| FF-SEC-016 | `NIT` | P3 | High | HTTP rate limiting | Accepted hardening: client buckets never expire. |
| FF-SEC-017 | `NIT` | P3 | High | HTTP server | Accepted hardening: body/write timeouts and upload concurrency are unbounded. |
| FF-SEC-018 | `NIT` | P3 | Medium | Runtime tool cache | Accepted hardening: cached executables are not rehashed before reuse. |
| FF-SEC-019 | `NIT` | P3 | High | Web/landing headers | Accepted hardening: no explicit CSP/frame/nosniff/referrer/permissions baseline. |

## Triage details

### FF-SEC-001 — DNS rebinding reaches the unauthenticated loopback API

- Evidence: `cmd/zv-orchestrator/config.go:68,101-103` leaves the default loopback bind tokenless; `internal/httpapi/routes.go:88-93` disables authentication when the token is empty; `internal/httpapi/middleware.go:22-38` accepts `Sec-Fetch-Site: same-origin` and compares `Origin` with the request-controlled `Host`.
- Exploit: an attacker-controlled hostname rebinding to `127.0.0.1:8080` remains same-origin to the browser and can use `Host: attacker.example:8080` for API reads, uploads, captures, renders, and deletes.
- Impact: disclosure and modification of local demos, media, plans, and job state, plus costly local operations.
- Fix: require a random per-session capability on loopback reads and writes; validate `Host` against the actual listener authority/explicit loopback literals; cover attacker-domain `Host` and same-origin fetch metadata in regression tests.

### FF-SEC-002 — Renderer-controlled approval becomes privileged execution

- Evidence: `desktop/src/preload.ts:30-46` exposes `status`, `send`, and `approve(actionId)` to the content renderer; `desktop/src/main.ts:781-785` verifies sender/frame/origin but not human interaction; `desktop/src/assistant/controller.ts:465-476,557-560` converts a pending ID to `privileged: true`; `desktop/src/studio-operations/operation-gateway.ts:98-110` executes non-read operations on that boolean.
- Exploit: renderer compromise or XSS can create/read a pending action and invoke `approve(id)` without a human click.
- Impact: deletion of jobs/media, approval of briefs, and capture/render execution without consent.
- Fix: keep pending executable state in `main` and authorize it only from an isolated native/privileged confirmation surface; do not rely on renderer `userActivation` or `isTrusted`.

### FF-SEC-003 — Arbitrary VOD URLs permit server-side requests

- Evidence: `internal/httpapi/stream_handlers.go:183-212` accepts `source_url`; `internal/vodfetch/vodfetch.go:92-126` accepts every HTTP(S) host as `SourceOther`; `internal/vodfetch/vodfetch.go:195-249` hands the URL to `yt-dlp`.
- Exploit: sources can target loopback, RFC1918, link-local/metadata, IPv6 local addresses, or redirect/rebind to them.
- Impact: blind interaction with local/LAN services and possible exfiltration when an internal response is accepted as media and later served as a stream source.
- Fix: prefer a provider/host allowlist; otherwise resolve and reject all non-global addresses on every connection and redirect, reject userinfo/unapproved ports, and test redirect and DNS-rebinding cases.

### FF-SEC-004 — Demo player names inject HLAE/CS2 console commands

- Evidence: demo names flow through `internal/parser/demo_kills.go:48-53`, `internal/parser/collector.go:119`, and `internal/recording/types.go:140-146`; `internal/recording/scriptgen.go:213-227` quotes the name but does not reject `;`; generated JavaScript splits every command with `item.cmd.split(';')` at `scriptgen.go:71-78`.
- Exploit: a target named `victim;quit;rest` creates an independent `quit` command despite being inside the intended `spec_player` argument.
- Impact: capture denial, altered configuration, and any stronger action exposed by available HLAE/CS2 console commands.
- Fix: represent commands as an array instead of a semicolon-serialized string; prefer `spec_player_by_accountid`; reject `;`, CR/LF, NUL, and controls as defense in depth.

### FF-SEC-005 — The orchestrator token follows cross-origin redirects

- Evidence: `web/app/api/demos/_lib.ts:20-27,48-55,101-105` adds `X-FragForge-Token` and uses the default `fetch` redirect behavior.
- Reproduction: two local Node HTTP servers showed that the installed runtime forwarded `X-FragForge-Token: audit-secret` from a 302 response to a different origin/port.
- Impact: a compromised or redirecting upstream can steal the server-side token and reuse it against a remotely bound orchestrator.
- Fix: set `redirect: "manual"` on authenticated calls and reject 3xx, or follow only after comparing the resolved `Location` origin with the configured orchestrator origin and rebuilding safe headers.

### FF-SEC-006 — Origin-less local clients can use the privileged Next proxy

- Evidence: `web/lib/api/local-request-guard.ts:12-26` deliberately accepts origin-less requests with a loopback `Host`; `web/middleware.ts:4-9` is the only proxy gate; mutation routes call the orchestrator with its server-side token.
- Exploit: any local process able to connect to the Next port can enumerate and mutate jobs without knowing `ORCHESTRATOR_TOKEN`.
- Impact: deletes, captures, renders, and CPU/disk use through a confused deputy.
- Triage note: severity is reduced because the current product is Windows-local and a same-user process often already has broad file/process access. It remains a real privilege-boundary gap for sandboxed or lower-integrity clients.
- Fix: require a renderer/session capability for proxy mutations; let trusted CLI clients address the orchestrator with their own token instead of inheriting proxy authority.

### FF-SEC-007 — Source URLs with credentials are persisted and disclosed

- Evidence: `internal/streamclips/types.go:60-74` exports `SourceURL`; `internal/httpapi/stream_handlers.go:201-213,258-284` stores and returns it; `cmd/zv-orchestrator/sqlite_stream_repo.go:60-78,189-219` persists the raw value. Error-only redaction in `internal/vodfetch/vodfetch.go:325-343` does not protect storage/API responses.
- Impact: userinfo, signed query strings, and private CDN tokens remain in SQLite, API JSON, and the renderer DOM.
- Fix: reject userinfo; separate a private acquisition URL from a redacted public DTO; remove query/fragment except explicitly allowed fields; clear or encrypt the acquisition secret after completion/failure.

### FF-SEC-008 — Next and sharp versions have current advisories

- Evidence: `web/pnpm-lock.yaml` resolves Next 15.5.19 and sharp 0.34.5; `landing/pnpm-lock.yaml` resolves Next 15.5.20 and sharp 0.34.5. Both production audits report 4 high and 5 moderate advisories.
- Advisories: Next `GHSA-m99w-x7hq-7vfj`, `GHSA-89xv-2m56-2m9x`, `GHSA-p9j2-gv94-2wf4`, `GHSA-68g3-v927-f742`, `GHSA-4633-3j49-mh5q`, `GHSA-4c39-4ccg-62r3`, `GHSA-q8wf-6r8g-63ch`, `GHSA-955p-x3mx-jcvp`; sharp `GHSA-f88m-g3jw-g9cj`.
- Reachability: no Server Actions, rewrites, Edge runtime, dynamic external `next/image`, or untrusted GIF/TIFF/VIPS processing was found. This is actionable patch debt rather than a demonstrated application exploit.
- Fix: move both apps to Next `>=15.5.21` and sharp `>=0.35.0` through a supported dependency update, then run the full web, landing, and desktop packaging gates.

### FF-SEC-009 — The public installer is unsigned

- Evidence: `desktop/package.json:63-73` configures NSIS without signing; `desktop/scripts/dist.mjs:34-47` creates and verifies only co-located SHA-256 values; `desktop/GUIDE.md:141-149` documents the unsigned release. `Get-AuthenticodeSignature` on the current 2.2.12 installer returned `NotSigned`.
- Impact: compromise of the release account or distribution location can replace the installer and checksum together; users have no publisher identity/root of trust.
- Fix: Authenticode-sign and timestamp the installer with a protected key, verify the chain before publishing, retain hashes as a secondary integrity control, and publish the expected signer identity.

### FF-SEC-010 — Non-loopback mode sends secrets and media over cleartext HTTP

- Evidence: `cmd/zv-orchestrator/config.go:101-103` permits a non-loopback bind when a mutation token exists; `cmd/zv-orchestrator/http_runtime.go:17-23,51-54` serves plain HTTP.
- Impact: a LAN observer can capture the token and media, then replay authenticated requests.
- Fix: reject non-loopback binds unless an explicit reverse-proxy mode is configured, or support TLS/mTLS directly. Document that a bearer token does not make HTTP confidential.

### FF-SEC-011 — Lua effects have no memory, source, callback, or output quota

- Evidence: `internal/editor/effects.go:51-63` reads an external script without a size cap; `effects.go:513-521` parses/compiles before the timeout; `effects.go:539-572` applies only a two-second execution context; `effects.go:585-642` appends callbacks/effects without cardinality limits.
- Impact: an external effects script can exhaust worker memory or generate excessive render-plan/FFmpeg work despite the CPU timeout.
- Fix: cap source bytes before parse, callback/effect counts and string sizes; isolate untrusted evaluation behind a process/Job Object with a memory limit if external scripts remain supported.

### FF-SEC-012 — `image.path` escapes the stated Lua filesystem/network sandbox

- Evidence: `internal/editor/effects.go:736-740` accepts any non-empty path; `internal/editor/ffmpeg.go:228-230` supplies it directly as an FFmpeg `-i` input.
- Impact: an external script can select local/device/UNC paths or FFmpeg-supported URLs, enabling local file-derived content or network/SMB requests, including possible credential leakage.
- Fix: restrict images to pre-authorized assets under a canonical root; reject schemes, UNC/device paths, symlink escapes, and unsafe FFmpeg protocols.

### FF-SEC-013 — Shell music bootstrap ignores pinned hashes

- Evidence: `data/music/catalog.json` supplies per-track SHA-256 values; `scripts/fetch-music.sh:28-40` trusts existing files or downloads directly to the destination and `:43-52` validates only duration. `scripts/run-local.sh:38` invokes this path.
- Impact: compromised upstream bytes or a partial/tampered cache are processed by ffprobe/FFmpeg and can enter renders.
- Fix: download to a temporary file, require and compare the catalog digest, atomically rename only after validation, and revalidate existing cache entries. Match `scripts/local-studio.ps1` and `desktop/src/music-library.ts`.

### FF-SEC-014 — CI and repository policy fail open on dependencies and secrets

- Evidence: `.github/workflows/ci.yml` has no PR trigger, landing dependency/build gate, dependency audit, `govulncheck`, `gosec`, or secret scan. Live repository checks on the audit date found `main` unprotected, no rulesets, and dependency/security/secret scanning disabled.
- Impact: the current high dependency advisories pass CI; an accidental direct push or committed credential has no mandatory preventive gate.
- Fix: enable dependency graph/Dependabot/security updates and secret push protection; add production audits for all three JS packages plus `govulncheck`; add landing build checks; protect `main` with required checks and no force/delete, reconciling this with the direct-main workflow.

### FF-SEC-015 — Landing environment files are not ignored

- Evidence: root `.gitignore` ignores only `/.env`; `landing/.gitignore` does not cover `.env*`; `git check-ignore landing/.env.local landing/.env.production` reports both as unignored. No current secret was detected.
- Impact: a future landing credential can be committed to the public repository, amplified by disabled secret scanning/push protection.
- Fix: add `landing/.env*` with an explicit `!.env.example` exception and enable repository push protection.

### FF-SEC-016 — Rate-limiter buckets grow forever

- Evidence: `internal/httpapi/middleware.go:52-103` stores a permanent map entry for every client IP and performs no TTL/LRU cleanup.
- Impact: a remotely exposed IPv6 service can accumulate memory and mutex contention through rotating source addresses.
- Fix: cap cardinality and evict idle entries; consider IPv6-prefix aggregation.

### FF-SEC-017 — HTTP body/write lifetime and upload concurrency are not bounded

- Evidence: `cmd/zv-orchestrator/http_runtime.go:12-23` sets only header and idle timeouts; stream upload limits reach multiple GiB.
- Impact: an authenticated slow client can retain connections/goroutines and upload resources for long periods.
- Fix: apply endpoint-aware body deadlines and upload concurrency limits; configure response/write deadlines compatible with intentional media streaming.

### FF-SEC-018 — Runtime-tool cache does not rehash executables

- Evidence: `desktop/src/runtime-tools.ts:137-145,174-178,323-334` trusts marker/existence checks and can fall back to a markerless legacy install after refresh failure.
- Impact: modification by another same-user process persists across launches. This is not an initial privilege escalation because such a process can already write the user's data.
- Fix: store and verify per-file SHA-256 manifests before every reuse and retire markerless fallback after migration.

### FF-SEC-019 — Web applications lack explicit security response headers

- Evidence: `web/next.config.mjs:7-17` has no header policy; landing has no Next header config. A live landing probe returned HSTS but no CSP, `nosniff`, frame restriction, referrer policy, or permissions policy.
- Impact: no standalone injection was found, so this is defense in depth; it increases the consequence of a future renderer/web injection.
- Fix: add compatible CSP and `frame-ancestors`, `X-Content-Type-Options`, `Referrer-Policy`, and `Permissions-Policy`; set `poweredByHeader: false`; test Next, Electron, Three.js, fonts, and media before enforcement.

## Rejected or downgraded candidates

- `gosec` integer conversions in RGBA and PCM paths: deliberate width conversion after bit shifting/bit reinterpretation, not attacker-controlled integer overflow.
- `gosec` “hardcoded credential” reports: constants contain environment-variable/header names, not credential values.
- `gosec` path/command candidates in storage, songs, FFmpeg, and fixed CLI delegation: reviewed paths/IDs are validated or operator-controlled; process calls use argv without a shell. No exploitable traversal or shell expansion was demonstrated.
- Multipart exhaustion candidates: affected handlers first wrap bodies with `http.MaxBytesReader`.
- Artifact-key traversal: storage rejects absolute/parent paths and HTTP identifiers use UUID/strict token validation.
- SQL injection: reviewed repository queries use placeholders.
- Direct Lua `os`/`io`/`package`/`debug` escape: the VM uses `SkipOpenLibs`, opens a narrow library set, and removes `dofile`, `loadfile`, and `require`.
- Archive extraction/Zip Slip: no user archive extraction path exists in the audited media pipeline; pinned HLAE extraction is hash-gated.
- Electron generic IPC, navigation, and permissions: sandbox/context isolation are enabled, popups/navigation are denied or constrained, and IPC payloads/senders are checked.
- Desktop build-only `brace-expansion`/`fast-uri` advisories: no attacker-controlled build input was demonstrated and `pnpm audit --prod` is clean; update the build toolchain, but do not classify these as a shipped runtime exploit.
- Current Next/sharp CVE-specific exploit paths: the vulnerable feature prerequisites were not found, so FF-SEC-008 is patch debt rather than proof of compromise.

## Initial verification record (pre-remediation)

| Command/check | Result |
| --- | --- |
| `go test ./internal/httpapi ./internal/vodfetch ./internal/recording -count=1` | Passed. |
| Focused web guard/body/publish tests | 15/15 passed. |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.4.0 ./...` | No vulnerabilities found. |
| `go mod verify` | All modules verified. |
| `go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 -fmt=json ./cmd/... ./internal/...` | Returned candidates; each accepted/rejected manually as described above. |
| `pnpm --dir web audit --prod --json` | 4 high, 5 moderate. |
| `pnpm --dir landing audit --prod --json` | 4 high, 5 moderate. |
| `pnpm --dir desktop audit --prod --json` | No production dependency entries/vulnerabilities. |
| Cross-origin Node redirect probe | Reproduced forwarding of `X-FragForge-Token`. |
| Known-prefix secret scan | No current private key/AWS/GitHub/Google/OpenAI/xAI credential found. |
| Current installer Authenticode check | `NotSigned`; local SHA matched the release checksum. |
| Live landing response-header probe | HSTS present; headers listed in FF-SEC-019 absent. |

## Recommended remediation order

1. P0: fix FF-SEC-001 and FF-SEC-002 together, then add hostile-origin/compromised-renderer tests.
2. P1: fix FF-SEC-003 through FF-SEC-005 and FF-SEC-007; these protect secrets and prevent attacker-controlled execution/fetch paths.
3. P1 supply chain: fix FF-SEC-008, FF-SEC-009, FF-SEC-013, and FF-SEC-014 before the next public installer.
4. P2: close the proxy, exposed-bind, Lua, and landing environment gaps.
5. P3: schedule the bounded-resource and response-header hardening items.

## Remediation status — 2026-07-23

The implementation graph and ownership are recorded in
`SECURITY_REMEDIATION_WORKFLOW.md`. Independent Go, media-sandbox, and
TypeScript/supply-chain reviewers inspected the integrated worktree. Their
actionable code findings were corrected before the final gates.

| Finding | Current status | Authoritative implementation/proof |
| --- | --- | --- |
| FF-SEC-001 | Closed in code and verified | Loopback-only listener, post-resolution listener validation, listener-bound `Host` validation, mandatory 64-hex capability for reads and mutations; hostile-host/auth tests and full Go race gate pass. |
| FF-SEC-002 | Closed in code and verified | Renderer can only request approval; Electron main owns the native confirmation and consumes approval after an affirmative result. Desktop unit suite passes. |
| FF-SEC-003 | Closed in code and verified | Exact HTTPS Twitch/YouTube policy, no userinfo/ports, public-address checks on connection/redirect, yt-dlp config disabled; SSRF, redirect, IPv4/IPv6, and rebinding regressions pass. |
| FF-SEC-004 | Closed in code and verified | HLAE schedule uses atomic command arrays, account IDs are preferred, unsafe fallback names are rejected, and no command is split on `;`. Recording tests pass. |
| FF-SEC-005 | Closed in code and verified | Authenticated orchestrator fetches use `redirect: "manual"` and convert 3xx to a generic 502 without forwarding `Location`. Web gates pass. |
| FF-SEC-006 | Closed in code and verified | Proxy mutations require a constant-time checked HttpOnly Strict cookie. Electron seeds it from main; standalone browser mode uses a separate POST bootstrap secret without URL/JS exposure. Process environments keep the three capabilities separated. |
| FF-SEC-007 | Closed in code and verified | Public DTOs contain only sanitized source URLs; private acquisition URLs are separate and cleared on terminal status in memory and SQLite, including legacy migration. Repository tests pass. |
| FF-SEC-008 | Closed in code and verified | Web and landing use Next 15.5.21 and Sharp 0.35.3; both production audits report no known vulnerabilities; full builds pass. |
| FF-SEC-009 | Release path fixed; current public artifact still open | `dist` now requires Authenticode configuration, SHA-256/RFC3161 signing, exact publisher verification, and publishes signer metadata. It fails closed without credentials. The already-public v2.2.12 installer remains unsigned because no authorized certificate/HSM or expected subject is configured. |
| FF-SEC-010 | Closed in code and verified | Config and the resolved listener reject non-loopback authorities; no cleartext remote mode remains. Tests cover config and post-bind rejection. |
| FF-SEC-011 | Closed in code and verified | Source/AST/callback/execution/effect/string/stack/registry/time limits are enforced. Loops, named/nested functions, dynamic loaders, Base/Table libraries, and unbounded string helpers are unavailable. Adversarial tests and the full editor suite pass. |
| FF-SEC-012 | Closed in code and verified | Image inputs must be regular files canonically contained below the external script directory; absolute, drive, URL, UNC/device, parent, and symlink escapes are rejected. |
| FF-SEC-013 | Closed in code and verified | Bash and PowerShell launchers revalidate cache hashes, discard unpinned entries, download to same-directory temporaries, verify before media probing, and promote atomically. Shell/PowerShell syntax checks pass. |
| FF-SEC-014 | Code/settings applied; publication pending | CI includes PR gates, audits, govulncheck, gosec, and gitleaks. GitHub now has Dependabot alerts/security updates, secret scanning/push protection, strict required checks, linear history, and force-push/delete disabled. The new workflow is not live until commit/push; admin enforcement remains off until its new check contexts exist. |
| FF-SEC-015 | Closed in code and verified | Root and landing ignore environment-secret files. |
| FF-SEC-016 | Closed in code and verified | Limiter implementation has TTL, a 4096-bucket cap, oldest eviction, and IPv6 `/64` aggregation. Production is loopback-only and does not enable the shared loopback bucket, avoiding unauthenticated starvation. |
| FF-SEC-017 | Closed in code and verified | HTTP read/control deadlines and two-slot multipart concurrency cover API, HTMX `/ui/jobs`, stream, and voice-profile uploads; media responses remain client-paced. Full Go and race gates pass. |
| FF-SEC-018 | Closed in code and verified | Cache reuse is rooted in code-pinned archive and canonical extracted-tree SHA-256 values, not the writable marker. A forged executable plus matching marker is rejected. HLAE and the immutable FragForge FFmpeg release asset were independently extracted and matched their pinned tree digests; desktop tests pass. |
| FF-SEC-019 | Implemented and built; hosted deployment pending | Web and landing configurations emit CSP, nosniff, frame, referrer, permissions, and powered-by protections. Local production builds pass; the currently hosted landing still lacks these headers until the tracked change is published and deployed. |

### Post-remediation verification

| Command/check | Result |
| --- | --- |
| `scripts/go-gate.sh --no-format --build --race --security` | Passed tests, vet, `zv check`, staticcheck, builds, and full race suite. |
| `govulncheck@v1.4.0 ./...` | No vulnerabilities found. |
| `gosec@v2.28.0` with the CI-documented false-positive families excluded | 221 files, 57,768 lines, zero issues. |
| Desktop lint, typecheck, full unit suite, and build | Passed. |
| Web lint, typecheck, full unit suite, build, and production audit | Passed; no known vulnerabilities. |
| Landing build and production audit | Passed; no known vulnerabilities. |
| `scripts/ci-check.sh` / actionlint | Passed. |
| Bash parse for `fetch-music.sh` and `run-local.sh`; PowerShell AST parse for `local-studio.ps1` | Passed. |
| `gitleaks` v8.30.0 against a clean source-only snapshot of the current worktree | Passed: 969 tracked/non-ignored source files were copied without generated/ignored artifacts; 6.54 MB were scanned with redaction and no leaks were found. The earlier 17.12 GB whole-workspace scan was discarded as artifact noise. |
| GitHub repository controls | Dependabot/security updates, secret scanning/push protection, and `main` protection are enabled; new check contexts await workflow publication. |
| Stable FFmpeg runtime asset | Uploaded to the v2.2.12 release; source SHA-256 and extracted-tree SHA-256 match the code pins. |

No capture or render was launched. No commit or push was performed. Operational
closure of FF-SEC-009, FF-SEC-014, and FF-SEC-019 requires, respectively, an
authorized signing identity and explicit authorization to publish the tracked
workflow/application changes and deploy the landing.
