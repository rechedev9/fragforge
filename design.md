# FragForge Codex Workbench Design

Status: proposed
Date: 2026-06-07
Owner: FragForge
Document type: product design + UX/UI specification

## 1. Vision

FragForge should stop feeling like a terminal pipeline and start feeling like a
local production desk for CS2 Shorts.

The operator should open one visual Workbench, drop a `.dem`, write the same
kind of prompt they currently give Codex, and then work through the full flow
with Codex as the visible operator assistant:

```text
.dem + prompt
  -> Codex understands the intent
  -> FragForge parses and explains the demo
  -> the operator approves expensive steps
  -> HLAE/CS2 records the right footage
  -> FFmpeg renders the selected pack
  -> Codex helps with captions, QA notes, and next actions
  -> final assets land in shortslistosparasubir
```

The interface is not a marketing page and not a generic video editor. It is a
focused operations tool for producing upload-ready CS2 clips with less terminal
work, less hidden state, and fewer repeated manual commands.

## 2. Product Goals

- Make `.dem + prompt` the first-class entry point.
- Preserve the current Codex collaboration pattern: the user states intent,
  Codex reasons from context, proposes next steps, and the user can approve or
  redirect.
- Keep deterministic tools in control of execution: Go workers, Asynq, HLAE,
  CS2, FFmpeg, Lua effects, and local storage remain the source of truth.
- Surface every important stage visually: upload, parsing, moments, recording,
  rendering, QA, captions, publish readiness, and cleanup.
- Make expensive or risky operations explicit and approved: HLAE/CS2 recording,
  long renders, and recycling demos.
- Deliver a professional, dense, operator-friendly UI suited to repeated
  production work.

## 3. Non-Goals

- Do not turn Codex into the parser, recorder, renderer, queue, or durable
  backend.
- Do not expose arbitrary shell commands from the browser.
- Do not build a Canva-style timeline editor in V1.
- Do not add cloud upload, account connection, or YouTube API integration in
  V1. The post-render assistant may prepare metadata, download the MP4, and open
  YouTube Studio, but publication remains manual.
- Do not replace `zv` CLI workflows. The UI should call stable API/task
  contracts that the CLI can continue to use.
- Do not hide raw artifacts from advanced users. The Workbench should reduce
  terminal use, not make the system opaque.

## 4. Target User

Primary user: the local FragForge operator.

They know the domain, understand CS2 demos, care about full HUD preservation,
and want output ready for Shorts/Reels/TikTok. They do not want to remember
long command lines, track directories manually, or inspect JSON unless something
needs debugging.

Secondary user: Codex itself, when acting as the local operator assistant.

Codex needs clean structured context, strict action contracts, durable result
artifacts, and clear boundaries so it can guide the workflow without becoming a
hidden process supervisor.

## 5. Experience Principles

### Operational, Not Decorative

The UI should look like a high-end production control surface: compact,
scannable, calm, and precise. Avoid oversized hero sections, marketing copy,
large decorative cards, gradient backgrounds, bokeh/orb decoration, and
one-note purple/blue themes.

### Codex Is Present, But Not Magical

Codex should be visible as a collaborator with messages, proposed actions, and
structured reasoning. It should not silently mutate job state. If an action is
expensive, destructive, or externally visible, it needs an approval button.

### State Must Be Obvious

Every job should answer these questions at a glance:

- What source did this come from?
- What did the prompt ask for?
- Which stage is active?
- What is blocked?
- What action is recommended next?
- What is already upload-ready?
- Where are the final files?

### Default To The Standard Clean POV

For kill/highlight Shorts, the visual default is `viral-60-clean`: clean
deathnotice-HUD capture rendered as 1080x1920 60fps POV with the standard
viral-ultra-clean overlay pack. Do not expose alternate render presets unless a
future product decision intentionally reopens the catalog.

### Review Before Cost

The UI should show parsed moments before recording, and recorded/rendered
artifacts before cleanup. Recording and rendering are slower than parsing, so
the operator should approve those steps from informed context.

## 6. Information Architecture

The Workbench has five persistent areas.

### 6.1 Header

Purpose: global health and local setup.

Content:

- Product name: `FragForge Workbench`
- Environment badge: `local`
- API health
- worker health summary
- mutation token control when required
- settings button

Header should stay compact. It should not become a navigation bar for unrelated
features.

### 6.2 Left Rail: Intake And Jobs

Purpose: create work and move between active runs.

Sections:

- New run
- Active intakes
- Recent jobs
- Filters: `all`, `needs input`, `recording`, `rendering`, `ready`, `failed`

Each list row should show:

- short id
- source filename or demo key
- target player when known
- stage badge
- last update time
- warning indicator when blocked

### 6.3 Center: Codex Operator

Purpose: preserve the conversational workflow.

Content:

- initial prompt
- user follow-up messages
- Codex responses
- proposed actions
- approval/reject controls
- questions that need user input
- stage timeline

The operator thread is not a chat toy. It is the command center for intent,
decision points, and next actions.

### 6.4 Right Rail: Review

Purpose: inspect the current job without leaving the Workbench.

Tabs:

- `Moments`
- `Recording`
- `Render`
- `Publish`
- `QA`
- `Artifacts`

The right rail changes based on selected job state. It should start with
Moments after parsing, then move toward Publish as the job matures.

### 6.5 Bottom Drawer: Logs And JSON

Purpose: advanced diagnostics without polluting the primary UI.

Collapsed by default. Opens when:

- the user clicks an artifact/log link
- a job fails
- Codex produces invalid JSON
- the operator chooses `Show details`

Content:

- worker logs
- Codex context/result JSON
- render state JSON
- publish board JSON
- copied command equivalents for advanced manual fallback

## 7. First Screen

The first screen must be usable immediately.

```text
+--------------------------------------------------------------------------------+
| FragForge Workbench       local | API ready | workers: parser, record, render   |
+---------------------+--------------------------------------+-------------------+
| New run             | Codex Operator                       | Review            |
|                     |                                      |                   |
| Drop .dem           | "Drop a demo and describe the result"| No job selected   |
| Prompt              |                                      |                   |
| [Start with Codex]  |                                      |                   |
|                     |                                      |                   |
| Active              |                                      |                   |
| - no active jobs    |                                      |                   |
+---------------------+--------------------------------------+-------------------+
```

Empty state copy should be short and direct:

- Drop a CS2 `.dem`.
- Describe the clip you want.
- Codex will propose the next step.

Avoid explanatory paragraphs inside the app. The app should teach through
controls, state, and concise labels.

## 8. Key User Flows

### 8.1 Happy Path: Known Player In Prompt

1. Operator drops `.dem`.
2. Operator writes: `Haz un Short largo con todas las kills de martinez.`
3. Codex proposes `inspect_players` if `martinez` needs resolution.
4. Workbench shows candidate players.
5. Operator confirms player.
6. Codex proposes `create_job`.
7. Backend creates job and enqueues parse.
8. Moments appear with scores and reason codes.
9. Codex proposes recording all selected moments.
10. Operator approves recording.
11. Recording completes.
12. Codex proposes `viral-60-clean`.
13. Operator approves render.
14. Publish pack appears with video, cover, caption, gallery, QA.
15. Codex proposes captions/hashtags and cleanup.
16. Operator prepares publication, downloads the MP4, and opens YouTube Studio.

### 8.2 Prompt With SteamID64

If the prompt includes a valid SteamID64, Codex can skip player discovery and
propose `create_job` immediately.

The UI should still show the target id and allow correction before recording.

### 8.3 Ambiguous Prompt

Example: `Haz algo bueno para Shorts.`

Codex should ask for the minimum missing decision:

- target player
- style: kills, utility, cheater POV, music sync
- output default if no preference: `viral-60-clean`

The UI should not block on unnecessary questions. If the user gives only a
target player, defaults are acceptable.

### 8.4 Failure Path

When a stage fails:

- the stage badge turns failed
- the right rail opens `QA` or `Artifacts`
- the Codex Operator summarizes the failure from structured context
- manual retry buttons appear only when the backend allows that transition
- logs are one click away

Codex failure should not fail the media job. Media failure should not hide
Codex controls.

## 9. Screen Specifications

### 9.1 New Run Panel

Controls:

- File drop zone for `.dem`
- Prompt textarea
- Optional target SteamID64 input hidden under `Advanced`
- Preset display hidden under `Advanced`; no alternate preset selector in V1
- Primary button: `Start with Codex`

Behavior:

- Drop zone validates extension and size before upload.
- Prompt can be empty only if target SteamID64 is provided.
- Upload progress appears inline.
- After upload, the intake appears in the active list and the operator thread is
  selected.

### 9.2 Codex Operator Panel

Message types:

- User message
- Codex response
- System event
- Action proposal
- Action result

Action proposal layout:

```text
Recommended action
Start recording
Records 12 selected moments with HLAE/CS2.

[Approve] [Reject] [Show details]
```

Rules:

- Use one primary action at a time.
- Secondary actions can appear below as quiet buttons.
- Expensive actions require approval.
- Completed actions stay visible in the timeline.
- Failed actions show the error and next valid retry.

### 9.3 Moments Tab

Purpose: choose what is worth recording.

Columns:

- select checkbox
- score
- round
- player
- weapon summary
- reason codes
- duration
- warnings

Interactions:

- select all recommended
- deselect individual moments
- sort by score, round, duration
- preview metadata before recording

Reason codes should be human-readable in UI:

- `multi_kill` -> `multi-kill`
- `headshot` -> `headshot`
- `awp` -> `AWP`
- `wallbang` -> `wallbang`
- `utility_lineup` -> `utility lineup`
- `recording_missing` -> `not recorded`

### 9.4 Recording Tab

Purpose: make the HLAE/CS2 stage visible.

Content:

- recording status
- selected segments count
- HLAE path health
- CS2 path health
- output segment list
- per-segment duration and probe status
- retry action when allowed

The UI must make it clear when CS2 is expected to open. Recording should never
start from an unlabeled background action.

### 9.5 Render Tab

Purpose: materialize output variants.

Content:

- loadout selector
- current render state
- output shape: `1080x1920`, `60fps`, `h264/aac`
- preset description
- render button
- generated videos and covers

Default loadout:

- `viral-60-clean`

For kill/highlight jobs, `viral-60-clean` should be preselected.

### 9.6 Publish Tab

Purpose: show upload readiness.

Content:

- publish board status
- upload-ready root: `shortslistosparasubir`
- video readiness
- cover readiness
- caption readiness
- gallery link
- manifest link
- publication assistant action

Statuses:

- `draft`
- `needs_cover`
- `needs_caption`
- `ready`
- `failed`

The `ready` state should be visually distinct and calm, not celebratory. This
is an operations tool.

### 9.7 QA Tab

Purpose: surface deterministic checks and Codex summaries.

Sections:

- media checks: resolution, duration, codec, FPS, filesize
- render warnings
- missing artifacts
- Codex QA summary
- logs

Warnings should be written as actions:

- `Video is 73s; Shorts target is <= 60s.`
- `Cover missing for seg-003.`
- `ffprobe failed; inspect render log.`

### 9.8 Artifacts Tab

Purpose: expose durable outputs.

Groups:

- demo source
- kill plan
- moments
- recording result
- render result
- edit document
- pack manifest
- captions
- covers
- videos
- logs

Each artifact row:

- name
- type
- readiness
- size when known
- open/download/copy key actions

## 10. Visual Design System

### Tone

Quiet, technical, premium. The interface should feel closer to a video
operations console than a social media tool.

### Layout

- Desktop default: three columns.
- Left rail fixed width: 300-360px.
- Center operator: flexible, minimum 420px.
- Right review rail: 420-520px.
- Bottom diagnostics drawer overlays the lower portion and can be resized.
- Mobile/tablet: stack into tabs: `Runs`, `Codex`, `Review`.

### Color

Use a restrained neutral base with functional accents.

Recommended palette:

```text
background:       #f6f7f9
surface:          #ffffff
surface-muted:    #f1f4f6
text:             #17202a
muted text:       #657282
border:           #d8dde5
accent:           #147c72
accent-hover:     #0f6c63
success:          #087443
warning:          #9a5b00
danger:           #b42318
focus ring:       #2f80ed
```

Avoid dominant purple, beige, dark blue, brown/orange, and heavy gradients.

### Typography

- System font stack.
- Base size: 14px.
- Dense panels: 13-14px.
- Section headings: 15-16px, semibold.
- Product title: 18-20px.
- Do not scale font size with viewport width.
- Letter spacing: `0`.

### Spacing And Density

Use an 8px spacing system.

Recommended values:

- page padding: 16px desktop, 10-12px mobile
- panel padding: 14-16px
- toolbar gap: 8px
- table row vertical padding: 8px
- list item padding: 10-12px
- panel radius: 8px maximum
- repeated item radius: 8px maximum

The interface should be dense enough for production work, but not so compressed
that controls compete. Important action proposals need a little more breathing
room than passive metadata rows.

### Components

Core components:

- `StatusBadge`
- `StageTimeline`
- `RunListItem`
- `DropZone`
- `PromptBox`
- `CodexMessage`
- `ActionProposal`
- `ApprovalBar`
- `MomentTable`
- `LoadoutSelector`
- `PublishBoard`
- `QualityReport`
- `ArtifactTable`
- `DiagnosticsDrawer`
- `WorkerHealth`

Cards should be reserved for repeated items, action proposals, and modal-like
surfaces. Do not nest cards inside cards.

### Component States

Every interactive component needs these states before V1 is considered polished:

- default
- hover
- focus
- active/pressed
- disabled
- loading
- success
- warning
- error

Important examples:

- `DropZone`: empty, dragging, uploading, uploaded, invalid file, upload failed.
- `ActionProposal`: proposed, approved, running, completed, rejected, failed.
- `StageTimeline`: pending, active, completed, blocked, failed.
- `MomentTable`: loading, empty, selectable, filtered, parse failed.
- `PublishBoard`: draft, needs cover, needs caption, ready, failed.
- `WorkerHealth`: ready, unavailable, misconfigured, unknown.

### Iconography

Use icons for compact controls when implementing with a frontend icon library:

- upload
- play/start
- pause/stop when needed
- refresh
- open folder
- download
- copy
- warning
- check
- settings
- external link

Text buttons are acceptable for major actions such as `Approve recording`,
`Render`, and `Open YouTube Studio`.

### Motion

Keep motion functional:

- upload progress
- stage transition pulse
- drawer open/close
- polling/SSE activity indicator

Avoid decorative animation.

### Responsive Behavior

Desktop is the primary target because recording/render review benefits from
screen space. Mobile support should still be coherent:

- `>= 1200px`: three-column workbench.
- `900px - 1199px`: left rail plus main area; review can become a right drawer.
- `< 900px`: tabbed layout with `Runs`, `Codex`, and `Review`.
- `< 560px`: single-column controls, full-width buttons, no horizontal table
  overflow without an explicit scroll container.

Tables should keep stable column widths on desktop. On mobile, dense tables can
switch to compact rows with labels, but they must not overflow the viewport.

### Perceived Performance

Long-running local work needs visible progress even when exact progress is not
available.

Use:

- upload percentage when available
- task stage labels during parse/record/render
- last-updated timestamp
- spinner only with adjacent text that names the work
- "still running" copy for long HLAE/FFmpeg phases
- worker health warnings before the user approves an action

Avoid generic loading states such as only `loading...` when the system knows the
current stage.

## 11. Interaction Rules

- One selected intake/job at a time.
- The selected item drives Codex, review tabs, and artifact drawer.
- The primary recommended next action appears in the Codex Operator panel.
- Buttons are disabled when the backend status does not allow the transition.
- Disabled controls must explain why through tooltip or adjacent reason text.
- Every mutating request includes the mutation token when configured.
- Polling should pause when the tab is hidden; SSE can replace polling later.
- UI state is advisory. Backend state wins after every refresh.

## 12. Content Design

Use direct operational copy.

Good labels:

- `Start with Codex`
- `Approve recording`
- `Render viral-60-clean`
- `Open gallery`
- `Show logs`
- `Open YouTube Studio`
- `Recycle demo`

Avoid vague labels:

- `Magic`
- `Enhance`
- `AI it`
- `Process`
- `Next`

Codex message style:

- concise
- state-aware
- explicit about missing inputs
- no exaggerated confidence
- no hidden promises

Example:

```text
I found two players matching "martinez". Pick the target before I create the
job. Parsing is cheap; recording will require approval later.
```

## 13. Backend Model

The current `jobs` table should keep its invariant: a production job has a
target SteamID64. Add an `intake` layer for `.dem + prompt` before that target
is known.

### Intake

```json
{
  "schema_version": "1.0",
  "id": "uuid",
  "status": "uploaded",
  "demo_key": "intakes/{id}/source.dem",
  "demo_sha256": "hex",
  "prompt": "user prompt",
  "linked_job_id": null,
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

Statuses:

- `uploaded`
- `inspecting`
- `awaiting_user`
- `ready_to_create_job`
- `linked`
- `failed`

### Operator Session

```json
{
  "schema_version": "1.0",
  "id": "uuid",
  "intake_id": "uuid",
  "job_id": "uuid-or-null",
  "status": "idle",
  "model": "gpt-5.4",
  "last_context_key": "intakes/{id}/agents/operator/context.json",
  "last_result_key": "intakes/{id}/agents/operator/result.json",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

Statuses:

- `idle`
- `thinking`
- `waiting_for_user`
- `waiting_for_approval`
- `running_action`
- `failed`

### Operator Message

```json
{
  "schema_version": "1.0",
  "id": "uuid",
  "session_id": "uuid",
  "role": "user",
  "content": "text",
  "artifact_refs": [],
  "created_at": "timestamp"
}
```

Roles:

- `user`
- `codex`
- `system`
- `action`

### Operator Action

```json
{
  "schema_version": "1.0",
  "id": "uuid",
  "session_id": "uuid",
  "kind": "start_render_variant",
  "status": "proposed",
  "requires_approval": true,
  "payload": {
    "job_id": "uuid",
    "variant": "viral-60-clean"
  },
  "result": null,
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

Statuses:

- `proposed`
- `approved`
- `running`
- `completed`
- `rejected`
- `failed`

## 14. API Additions

```text
POST /api/intakes
GET  /api/intakes
GET  /api/intakes/{id}

GET  /api/intakes/{id}/session
GET  /api/intakes/{id}/messages
POST /api/intakes/{id}/messages

GET  /api/intakes/{id}/actions
POST /api/intakes/{id}/actions/{action_id}/approve
POST /api/intakes/{id}/actions/{action_id}/reject

GET  /api/intakes/{id}/players
GET  /api/intakes/{id}/events
```

`POST /api/intakes` accepts multipart:

- `demo`: `.dem`
- `prompt`: text
- optional `config.target_steamid`
- optional `config.rules`

If `target_steamid` is present, Codex can propose `create_job` immediately. If
not, Codex should propose player inspection first.

`GET /api/intakes/{id}/events` can start as polling-compatible JSON. Server-Sent
Events are the preferred upgrade for live progress.

## 15. Codex Integration

Codex runs as a bounded local agent task, not a backend replacement.

Task kinds:

- `operator-plan`: produce message, questions, and proposed actions.
- `caption-candidates`: existing title/caption/hashtag generation.
- `qa-summary`: summarize deterministic QA and publish readiness.

The worker should:

- use `codex exec`
- use read-only sandbox by default
- use `--ephemeral`
- use `--output-last-message`
- apply `ZV_AGENT_TIMEOUT`
- pass compact JSON context
- store context and result artifacts
- reject invalid JSON for action-producing tasks
- never include `.env`, private keys, tokens, credential URLs, or local secrets

Codex output for `operator-plan`:

```json
{
  "message": "I found the target player and can create the job.",
  "questions": [],
  "actions": [
    {
      "kind": "create_job",
      "label": "Create job",
      "requires_approval": false,
      "payload": {
        "target_steamid": "76561198148986856",
        "rules": {}
      }
    }
  ],
  "warnings": []
}
```

Allowed action kinds:

- `inspect_players`
- `create_job`
- `start_recording`
- `start_composition`
- `start_render_variant`
- `start_caption_agent`
- `request_demo_cleanup`

Codex cannot emit raw commands. The backend validates every action kind,
payload, artifact key, status transition, and approval requirement.

## 16. Action Contracts

### `inspect_players`

Purpose: identify demo participants before a job exists.

Approval: no.

Payload:

```json
{}
```

Result:

```json
{
  "players": [
    {
      "name": "martinez",
      "steamid64": "76561198148986856",
      "team": "T"
    }
  ]
}
```

### `create_job`

Purpose: convert intake into a production job.

Approval: no when target is explicit or confirmed.

Payload:

```json
{
  "target_steamid": "76561198148986856",
  "rules": {},
  "prompt_summary": "all target kills, viral-60-clean"
}
```

Backend behavior:

- reuse the stored intake demo
- create a `jobs` row
- enqueue `parse:demo`
- link `intake.linked_job_id`

### `start_recording`

Purpose: enqueue HLAE/CS2 recording.

Approval: yes.

Payload:

```json
{
  "job_id": "uuid"
}
```

Guardrails:

- job must be `parsed` or `recorded`
- kill plan must exist
- HLAE path must be configured
- use `C:\HLAE-2.190.1\HLAE.exe` on this machine
- CS2 launch args must include `-windowed`

### `start_render_variant`

Purpose: render an upload-ready pack.

Approval: yes.

Payload:

```json
{
  "job_id": "uuid",
  "variant": "viral-60-clean"
}
```

Guardrails:

- variant must be from the loadout catalog
- job must be `recorded`, `composed`, or `done`
- default kill/highlight variant is `viral-60-clean`

### `start_caption_agent`

Purpose: generate title, caption, hashtag, and notes candidates.

Approval: no by default.

Payload:

```json
{
  "job_id": "uuid",
  "variant": "viral-60-clean"
}
```

### `request_demo_cleanup`

Purpose: recycle used local `.dem` copies after final output is validated.

Approval: yes.

Payload:

```json
{
  "demo_keys": ["intakes/{id}/source.dem"],
  "reason": "final publish pack validated"
}
```

Cleanup must use the Windows Recycle Bin for local files. It must not
permanently delete `.dem` files.

## 17. State Model

```text
intake.uploaded
  -> operator.thinking
  -> inspect_players?
  -> intake.awaiting_user?
  -> create_job
  -> job.queued
  -> job.parsing
  -> job.parsed
  -> operator proposes recording
  -> job.recording
  -> job.recorded
  -> operator proposes render
  -> render.queued
  -> render.rendering
  -> render.ready
  -> caption agent / QA summary
  -> publish.ready
  -> cleanup approval
```

The UI should show this as a timeline with durable event rows, not as a hidden
state enum.

## 18. Security And Operations

Local defaults:

- bind to `127.0.0.1:8080`
- require `ZV_MUTATION_TOKEN` for non-loopback binds
- deny CORS by default
- load no third-party scripts in the embedded Workbench
- cap upload size
- validate `.dem` upload field names, sizes, and content paths server-side

Command safety:

- no browser-submitted shell commands
- no arbitrary local paths
- no arbitrary render variants
- no arbitrary agent kinds
- no environment variable dumps
- no secrets in diagnostics

Operational safety:

- show worker health before action approval
- warn when record/render workers are not configured
- make slow/expensive actions explicit
- keep idempotent retry semantics for parse, compose, render, and caption tasks
- do not auto-retry recording without operator approval

## 19. Accessibility

Minimum bar:

- keyboard navigation for all controls
- visible focus ring
- labels for file picker, prompt, token, buttons, selects, and tabs
- table headers for all tabular data
- status text in addition to color
- sufficient contrast for badges and buttons
- no layout shift when status labels change
- no text overflow in compact panels
- reduced-motion friendly transitions

## 20. Implementation Plan

### Phase 1: Product Shell

- Upgrade embedded Workbench layout to left rail, Codex center, review rail.
- Add New Run panel with `.dem` upload and prompt.
- Keep existing Jobs/Moments/Render functionality available.
- Add smoke tests for upload/prompt UI presence.

### Phase 2: Intake Backend

- Add `internal/intake` domain types.
- Add repository and migrations for intakes, operator sessions, messages, and
  actions.
- Add `POST /api/intakes`, list, get, messages, and actions endpoints.
- Preserve the existing `/api/jobs` invariant.

### Phase 3: Player Discovery

- Add `inspect_players` action.
- Store player discovery artifacts.
- Add player picker UI for ambiguous prompts.
- Let confirmed target create a job.

### Phase 4: Codex Operator

- Extend `agent:codex` with `operator-plan`.
- Add structured context builder.
- Add strict result parser.
- Render Codex messages and action proposals in the Workbench.

### Phase 5: Action Bridge

- Implement approve/reject endpoints.
- Implement allowlisted action executors.
- Add approval gates for recording, rendering, and cleanup.
- Add tests for rejected transitions and malformed payloads.

### Phase 6: Review Quality

- Improve Moments, Recording, Render, Publish, QA, and Artifacts tabs.
- Add `qa-summary` Codex task.
- Add diagnostics drawer.
- Add direct gallery/open/download/copy-key actions.

### Phase 7: Local Launcher

- Add `zv workbench` or a Windows launcher that checks services and opens the
  local URL.
- Keep launcher separate from Codex and from worker process supervision.

## 21. Acceptance Criteria

V1 is good enough when the operator can:

1. Open the local Workbench.
2. Upload a `.dem`.
3. Write a prompt.
4. See Codex interpret the request and propose the next valid action.
5. Resolve a missing target player visually.
6. Create a production job without using the terminal.
7. Review parsed moments before recording.
8. Approve recording from the browser.
9. Approve rendering from the browser.
10. Review upload-ready video, cover, caption, gallery, pack manifest, and QA.
11. Find final outputs under `shortslistosparasubir`.
12. Prepare publication, download the MP4, and open YouTube Studio.
13. Approve demo cleanup only after validation.

Quality bar:

- The first screen is an operator workbench, not a landing page.
- The UI is dense but not cramped.
- All expensive actions have explicit approvals.
- All failed states show practical next steps.
- Codex actions are structured, auditable, and allowlisted.
- The backend remains the source of truth.

## 22. Design QA Checklist

Run this checklist before calling the UI slice ready:

- First viewport clearly shows `.dem` upload, prompt, Codex Operator, and review
  area.
- There is no landing-page hero, marketing copy, decorative orb, or unrelated
  visual flourish.
- Header, rails, tables, tabs, drawers, and action proposals align to one
  spacing system.
- Text fits inside every button, badge, table cell, and compact panel at desktop
  and mobile widths.
- Every status uses text plus color, not color alone.
- Every primary action has one clear owner: user approval, Codex proposal, or
  backend state.
- Expensive actions cannot be triggered accidentally.
- Failed states show the failure reason and the next valid action.
- The `shortslistosparasubir` destination is visible in Publish and Artifacts.
- The default kill/highlight render is `viral-60-clean`.
- The visual design is neutral and operational, not a one-color theme.
- The app can be used with keyboard focus visible.
- The UI remains useful if Codex is unavailable.
- The UI remains useful if render workers are unavailable.
- The browser never receives raw shell commands or secret-bearing context.

## 23. Open Decisions

- Embedded HTML vs Next.js: use embedded HTML for the first useful Workbench;
  move to Next.js only when component complexity justifies a build pipeline.
- Polling vs SSE: polling is acceptable for the first slice; SSE is preferred
  once long-running progress needs to feel live.
- Intake storage shape: keep intake separate from jobs to avoid nullable
  `target_steamid` in production jobs.
- Auto-run scope: allow parser-only and player-inspection actions to run
  automatically when safe; require approval for HLAE/CS2, long renders,
  and cleanup.
