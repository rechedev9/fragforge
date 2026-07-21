# Full HUD Native Killfeed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make vertical `full-hud-60` captures use a target-only, portrait-safe native HLAE killfeed while preserving the complete gameplay HUD and eliminating the FFmpeg killfeed overlay.

**Architecture:** Reuse the persisted `PortraitSafeKillfeed` recording-profile flag across both killfeed-capable HUD modes.
The HTTP layer admits the flag, the worker propagates it into recorder arguments and artifact reuse, the recording package configures HLAE independently from HUD visibility, and the editor trusts the persisted flag to keep native notices.

**Tech Stack:** Go 1.26.5, HLAE `mirv_deathmsg`, CS2 console commands, Asynq task payloads, FFmpeg manifest generation, and Go's standard `testing` package.

## Global Constraints

- Keep `cl_draw_only_deathnotices 0` for `gameplay` mode so Full HUD remains Full HUD.
- Filter the native killfeed to kills whose attacker is the selected target player.
- Enable portrait-safe positioning only for 9:16 killfeed-capable presets.
- Keep `clean-pov-60` and landscape Full HUD behavior unchanged.
- Keep the legacy render-time overlay for persisted recordings that do not carry `PortraitSafeKillfeed`.
- Do not add dependencies or run `go mod tidy`.
- Do not launch HLAE, CS2, or a long FFmpeg render without separate explicit approval.
- Do not commit or push unless the user separately requests it.
- Preserve unrelated worktree changes.

## File Map

- `internal/recording/types.go` owns normalization and validation of the persisted recording profile.
- `internal/recording/types_test.go` proves portrait-safe gameplay receives deterministic defaults and rejects incompatible clean HUD requests.
- `internal/recording/scriptgen.go` owns HLAE setup and cleanup commands.
- `internal/recording/scriptgen_test.go` proves Full HUD visibility and target-only native killfeed commands coexist.
- `internal/httpapi/handlers.go` derives the capture profile from preset capabilities and output format.
- `internal/httpapi/handlers_test.go` covers explicit record admission.
- `internal/httpapi/handlers_generate_test.go` covers one-click generate admission.
- `internal/workers/media_worker.go` propagates the admitted profile to the recorder and artifact-reuse check.
- `internal/workers/media_worker_test.go` covers recorder CLI arguments.
- `internal/workers/media_worker_segment_select_test.go` covers recapture and merge behavior when a Full HUD profile changes.
- `internal/editor/manifest.go` decides whether a vertical render needs the legacy killfeed overlay.
- `internal/editor/manifest_test.go` proves new native Full HUD captures skip the overlay while legacy captures retain it.

---

### Task 1: Extend The Recording Profile Contract

**Files:**
- Modify: `internal/recording/types.go:182-224, 247-275`
- Test: `internal/recording/types_test.go:82-124`

**Interfaces:**
- Consumes: `StreamConfig.HUDMode` and `StreamConfig.PortraitSafeKillfeed`.
- Produces: A normalized `StreamConfig` in which portrait-safe `gameplay` has the same deterministic safe-zone and lifetime values as portrait-safe `deathnotices`.
- Produces: `RecordingPlan.Validate() error` rejection for `PortraitSafeKillfeed` combined with any HUD mode other than `gameplay` or `deathnotices`.

- [ ] **Step 1: Replace the deathnotices-only defaults test and add the invalid-combination test**

Replace `TestNewPlanDeathnoticesPortraitDefaults` and append the validation test with the following code:

```go
func TestNewPlanPortraitSafeKillfeedDefaults(t *testing.T) {
	for _, hudMode := range []HUDMode{HUDModeDeathnotices, HUDModeGameplay} {
		t.Run(string(hudMode), func(t *testing.T) {
			kp := killplan.NewPlan()
			kp.Demo.Tickrate = 64
			kp.Target.SteamID64 = "76561198148986856"
			kp.Segments = []killplan.Segment{{ID: "seg-001", TickStart: 100, TickEnd: 200}}
			stream := DefaultStreamConfig()
			stream.HUDMode = hudMode
			stream.PortraitSafeKillfeed = true

			plan, err := NewPlanFromKillPlan(kp, "x.dem", "out", stream)
			if err != nil {
				t.Fatalf("NewPlanFromKillPlan error = %v", err)
			}
			if got, want := plan.Stream.DeathnoticeSafeZoneX, defaultDeathnoticeSafeZoneX; got != want {
				t.Fatalf("DeathnoticeSafeZoneX = %.2f, want %.2f", got, want)
			}
			if got, want := plan.Stream.DeathnoticeSafeZoneY, defaultDeathnoticeSafeZoneY; got != want {
				t.Fatalf("DeathnoticeSafeZoneY = %.2f, want %.2f", got, want)
			}
			if got, want := plan.Stream.DeathnoticeLifetime, defaultDeathnoticeLifetimeSeconds; got != want {
				t.Fatalf("DeathnoticeLifetime = %.2f, want %.2f", got, want)
			}
		})
	}
}

func TestValidateRejectsPortraitSafeKillfeedWithCleanHUD(t *testing.T) {
	p := testPlan()
	p.Stream.HUDMode = HUDModeClean
	p.Stream.PortraitSafeKillfeed = true

	err := p.Validate()
	if err == nil || !strings.Contains(err.Error(), "portrait_safe_killfeed") {
		t.Fatalf("Validate error = %v, want portrait_safe_killfeed HUD error", err)
	}
}
```

Add `"strings"` to the test imports.

- [ ] **Step 2: Run the focused contract tests and confirm the red state**

Run:

```powershell
go test ./internal/recording -run 'Test(NewPlanPortraitSafeKillfeedDefaults|ValidateRejectsPortraitSafeKillfeedWithCleanHUD)$' -count=1
```

Expected: FAIL because gameplay does not receive safe-zone or lifetime defaults and clean HUD is not rejected.

- [ ] **Step 3: Generalize normalization and validation in `types.go`**

After HUD-mode validation in `RecordingPlan.Validate`, add:

```go
	if p.Stream.PortraitSafeKillfeed && p.Stream.HUDMode != HUDModeGameplay && p.Stream.HUDMode != HUDModeDeathnotices {
		return fmt.Errorf("stream portrait_safe_killfeed requires hud_mode %q or %q", HUDModeGameplay, HUDModeDeathnotices)
	}
```

Replace the target validation condition with:

```go
	if p.Stream.HUDMode == HUDModeDeathnotices || p.Stream.PortraitSafeKillfeed {
		if _, err := AccountIDFromSteamID64(p.TargetSteamID64); err != nil {
			return fmt.Errorf("filtered deathnotices target_steamid64: %w", err)
		}
	}
```

Replace the deathnotices-only normalization block with:

```go
	if stream.PortraitSafeKillfeed {
		if stream.DeathnoticeSafeZoneX == 0 {
			stream.DeathnoticeSafeZoneX = defaultDeathnoticeSafeZoneX
		}
		if stream.DeathnoticeSafeZoneY == 0 {
			stream.DeathnoticeSafeZoneY = defaultDeathnoticeSafeZoneY
		}
	}
	if stream.HUDMode == HUDModeDeathnotices || stream.PortraitSafeKillfeed {
		if stream.DeathnoticeLifetime == 0 {
			stream.DeathnoticeLifetime = defaultDeathnoticeLifetimeSeconds
		}
	}
```

- [ ] **Step 4: Run the focused contract tests and the complete recording package**

Run:

```powershell
go test ./internal/recording -run 'Test(NewPlanPortraitSafeKillfeedDefaults|ValidateRejectsPortraitSafeKillfeedWithCleanHUD)$' -count=1
go test ./internal/recording -count=1
```

Expected: PASS for both commands.

- [ ] **Step 5: Record the review checkpoint without committing**

Run `git diff -- internal/recording/types.go internal/recording/types_test.go` and confirm only profile normalization, validation, and their tests changed.
Do not run `git commit` without separate user authorization.

---

### Task 2: Configure HLAE For Portrait-Safe Gameplay

**Files:**
- Modify: `internal/recording/scriptgen.go:194-206, 304-354`
- Test: `internal/recording/scriptgen_test.go:230-311`

**Interfaces:**
- Consumes: The normalized `StreamConfig` contract from Task 1.
- Produces: `hudSetupCommands(RecordingPlan) []string` that keeps HUD visibility separate from native killfeed filtering.
- Produces: `hudCleanupCommands(StreamConfig) []string` that restores every HLAE and safe-zone mutation made by setup.

- [ ] **Step 1: Add the portrait-safe gameplay script regression test**

Add this test after `TestGenerateHLAEJavaScriptGameplayHUDIsDefault`:

```go
func TestGenerateHLAEJavaScriptPortraitSafeGameplayHUD(t *testing.T) {
	p := testPlan()
	p.Stream.HUDMode = HUDModeGameplay
	p.Stream.PortraitSafeKillfeed = true

	js, err := GenerateHLAEJavaScript(p)
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	for _, want := range []string{
		`cl_drawhud 1`,
		`cl_draw_only_deathnotices 0`,
		`mirv_deathmsg clear`,
		`mirv_deathmsg filter clear`,
		`mirv_deathmsg filter add attackerMatch=!x76561198148986856 block=1 lastRule=1`,
		`mirv_deathmsg localPlayer -1`,
		`mirv_deathmsg lifetime 1.6`,
		`safezonex 0.28`,
		`safezoney 0.82`,
		`mirv_deathmsg localPlayer default`,
		`mirv_deathmsg lifetime default`,
		`safezonex 1`,
		`safezoney 1`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("generated JS missing %q\n%s", want, js)
		}
	}
	if strings.Contains(js, `cl_draw_only_deathnotices 1`) {
		t.Fatalf("portrait-safe gameplay hid the full HUD:\n%s", js)
	}
}
```

Strengthen `TestGenerateHLAEJavaScriptGameplayHUDIsDefault` with:

```go
	if strings.Contains(js, `mirv_deathmsg`) || strings.Contains(js, `safezonex`) || strings.Contains(js, `safezoney`) {
		t.Fatalf("plain gameplay unexpectedly configures portrait killfeed:\n%s", js)
	}
```

- [ ] **Step 2: Run the script tests and confirm the red state**

Run:

```powershell
go test ./internal/recording -run 'TestGenerateHLAEJavaScript(PortraitSafeGameplayHUD|GameplayHUDIsDefault)$' -count=1
```

Expected: FAIL because gameplay currently returns before adding `mirv_deathmsg` and safe-zone commands.

- [ ] **Step 3: Make HUD setup append filtering independently from HUD visibility**

Change the cleanup call in `buildSchedule` to:

```go
	for i, cmd := range hudCleanupCommands(plan.Stream) {
```

Replace `hudSetupCommands` and `hudCleanupCommands` with:

```go
func hudSetupCommands(plan RecordingPlan) []string {
	var commands []string
	switch plan.Stream.HUDMode {
	case HUDModeClean:
		return []string{
			"spec_show_xray 0; cl_drawhud 0",
		}
	case HUDModeDeathnotices:
		commands = []string{
			"spec_show_xray 0",
			"cl_spec_show_bindings 0",
			"cl_drawhud 1",
			"cl_draw_only_deathnotices 1",
			"cl_show_observer_crosshair 2",
			"crosshair 1",
		}
	default:
		commands = []string{
			"spec_show_xray 0",
			"cl_spec_show_bindings 0",
			"cl_drawhud 1",
			"cl_draw_only_deathnotices 0",
			"cl_show_observer_crosshair 2",
			"crosshair 1",
		}
	}
	if plan.Stream.HUDMode != HUDModeDeathnotices && !plan.Stream.PortraitSafeKillfeed {
		return commands
	}
	commands = append(commands,
		"mirv_deathmsg clear",
		"mirv_deathmsg filter clear",
		fmt.Sprintf("mirv_deathmsg filter add attackerMatch=!x%s block=1 lastRule=1", plan.TargetSteamID64),
		"mirv_deathmsg localPlayer -1",
		fmt.Sprintf("mirv_deathmsg lifetime %s", formatFloat(plan.Stream.DeathnoticeLifetime)),
	)
	if plan.Stream.PortraitSafeKillfeed {
		commands = append(commands,
			fmt.Sprintf("safezonex %s", formatFloat(plan.Stream.DeathnoticeSafeZoneX)),
			fmt.Sprintf("safezoney %s", formatFloat(plan.Stream.DeathnoticeSafeZoneY)),
		)
	}
	return commands
}

func hudCleanupCommands(stream StreamConfig) []string {
	if stream.HUDMode != HUDModeDeathnotices && !stream.PortraitSafeKillfeed {
		return nil
	}
	return []string{
		"mirv_deathmsg clear",
		"mirv_deathmsg filter clear",
		"mirv_deathmsg localPlayer default",
		"mirv_deathmsg lifetime default",
		"safezonex 1",
		"safezoney 1",
	}
}
```

- [ ] **Step 4: Run all script and recording tests**

Run:

```powershell
go test ./internal/recording -run 'TestGenerateHLAEJavaScript(PortraitSafeGameplayHUD|GameplayHUDIsDefault|DeathnoticesHUDMode|LandscapeDeathnoticesKeepNativeSafeZone)$' -count=1
go test ./internal/recording -count=1
```

Expected: PASS, including unchanged deathnotices and plain gameplay behavior.

- [ ] **Step 5: Record the review checkpoint without committing**

Run `git diff -- internal/recording/scriptgen.go internal/recording/scriptgen_test.go` and confirm that setup and cleanup are the only production behavior changes.
Do not run `git commit` without separate user authorization.

---

### Task 3: Admit Portrait-Safe Full HUD Requests

**Files:**
- Modify: `internal/httpapi/handlers.go:779-817, 879-905`
- Test: `internal/httpapi/handlers_test.go:1479-1527`
- Test: `internal/httpapi/handlers_generate_test.go:307-327`

**Interfaces:**
- Consumes: `editor.RenderPreset.KillfeedSource`, normalized `renderplan.EditRequest.Format`, and existing task constructors.
- Produces: `tasks.RecordDemoPayload{HUDMode: "gameplay", PortraitSafeKillfeed: true}` for vertical `full-hud-60` requests.
- Preserves: `PortraitSafeKillfeed: false` for `clean-pov-60` and all landscape requests.

- [ ] **Step 1: Expand explicit-record and generate tests into the required matrix**

Update the explicit-record table to include:

```go
		{name: "kill feed vertical", preset: editor.PresetViral60Clean, format: renderplan.FormatShort9x16, wantHUD: "deathnotices", wantPortraitSafeKillfeed: true},
		{name: "kill feed landscape", preset: editor.PresetViral60Clean, format: renderplan.FormatLandscape16x9, wantHUD: "deathnotices"},
		{name: "clean POV", preset: editor.PresetCleanPOV60, format: renderplan.FormatShort9x16, wantHUD: "clean"},
		{name: "full HUD vertical", preset: editor.PresetFullHUD60, format: renderplan.FormatShort9x16, wantHUD: "gameplay", wantPortraitSafeKillfeed: true},
		{name: "full HUD landscape", preset: editor.PresetFullHUD60, format: renderplan.FormatLandscape16x9, wantHUD: "gameplay"},
```

Replace `TestStartGenerateVerticalKillfeedRequestsPortraitSafeCapture` with:

```go
func TestStartGenerateKillfeedCaptureProfile(t *testing.T) {
	tests := []struct {
		name                     string
		preset                   string
		format                   string
		wantHUD                  string
		wantPortraitSafeKillfeed bool
	}{
		{name: "kill feed vertical", preset: editor.PresetViral60Clean, format: renderplan.FormatShort9x16, wantHUD: "deathnotices", wantPortraitSafeKillfeed: true},
		{name: "full HUD vertical", preset: editor.PresetFullHUD60, format: renderplan.FormatShort9x16, wantHUD: "gameplay", wantPortraitSafeKillfeed: true},
		{name: "full HUD landscape", preset: editor.PresetFullHUD60, format: renderplan.FormatLandscape16x9, wantHUD: "gameplay"},
		{name: "clean POV vertical", preset: editor.PresetCleanPOV60, format: renderplan.FormatShort9x16, wantHUD: "clean"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			store := newFakeStorage()
			queue := &fakeQueue{}
			plan := killplan.NewPlan()
			j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
			repo.jobs[j.ID] = j
			h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

			body := fmt.Sprintf(`{"preset":%q,"edit":{"format":%q}}`, tc.preset, tc.format)
			rw := postGenerate(t, h, j.ID, body)
			if rw.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
			}
			var payload tasks.RecordDemoPayload
			if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
				t.Fatalf("unmarshal record payload: %v", err)
			}
			if payload.HUDMode != tc.wantHUD || payload.PortraitSafeKillfeed != tc.wantPortraitSafeKillfeed {
				t.Fatalf("record payload = %#v, want HUD %q portrait-safe %t", payload, tc.wantHUD, tc.wantPortraitSafeKillfeed)
			}
		})
	}
}
```

- [ ] **Step 2: Run admission tests and confirm the red state**

Run:

```powershell
go test ./internal/httpapi -run 'TestStart(RecordingAppliesPresetCaptureHUD|GenerateKillfeedCaptureProfile)$' -count=1
```

Expected: FAIL only for vertical Full HUD because the handler currently restricts the flag to `deathnotices` mode.

- [ ] **Step 3: Derive portrait safety from preset capability in both handlers**

In `StartRecording`, replace the portrait-safe assignment with:

```go
				portraitSafeKillfeed = preset.KillfeedSource && edit.Format == renderplan.FormatShort9x16
```

In `StartGenerate`, replace the portrait-safe assignment with:

```go
	portraitSafeKillfeed := preset.KillfeedSource && intent.Edit.Format == renderplan.FormatShort9x16
```

- [ ] **Step 4: Run the focused and complete HTTP API tests**

Run:

```powershell
go test ./internal/httpapi -run 'TestStart(RecordingAppliesPresetCaptureHUD|GenerateKillfeedCaptureProfile)$' -count=1
go test ./internal/httpapi -count=1
```

Expected: PASS for both commands.

- [ ] **Step 5: Record the review checkpoint without committing**

Run `git diff -- internal/httpapi/handlers.go internal/httpapi/handlers_test.go internal/httpapi/handlers_generate_test.go` and verify that no request schema or endpoint changed.
Do not run `git commit` without separate user authorization.

---

### Task 4: Propagate The Profile And Force Correct Recapture

**Files:**
- Modify: `internal/workers/media_worker.go:492-562`
- Test: `internal/workers/media_worker_test.go:125-175`
- Test: `internal/workers/media_worker_segment_select_test.go:215-264`

**Interfaces:**
- Consumes: `tasks.RecordDemoPayload.PortraitSafeKillfeed` admitted by Task 3.
- Produces: `normalizedRecordingStream(..., portraitSafeKillfeed)` using the exact requested profile.
- Produces: `--portrait-safe-killfeed` recorder CLI argument for both gameplay and deathnotices modes.
- Preserves: Stream equality as the artifact compatibility boundary, which forces old gameplay recordings to recapture.

- [ ] **Step 1: Add gameplay propagation and recapture coverage**

Add this row to `TestRecordWorkerHUDFromPayloadOverridesDefault`:

```go
		{name: "vertical full HUD configures portrait safe capture", hud: "gameplay", portraitSafeKillfeed: true, wantHUD: "gameplay", wantPortraitFlag: true},
```

Rename `TestRecordWorkerInvalidatesAndDoesNotMergeAcrossCaptureProfiles` to `TestRecordWorkerInvalidatesGameplayCaptureWhenPortraitSafetyChanges`.
Within that test, replace all four `"deathnotices"` task HUD arguments with `"gameplay"` and update the opening comment to:

```go
	// The old Full HUD profile records one segment without a portrait-safe native
	// killfeed. A later portrait-safe Full HUD request must not reuse or merge it.
```

- [ ] **Step 2: Run worker regressions and confirm the red state**

Run:

```powershell
go test ./internal/workers -run 'TestRecordWorker(HUDFromPayloadOverridesDefault|InvalidatesGameplayCaptureWhenPortraitSafetyChanges)$' -count=1
```

Expected: FAIL because the worker strips portrait safety from gameplay profiles and omits the recorder flag.

- [ ] **Step 3: Stop restricting the flag to deathnotices inside the worker**

Replace the expected-stream block with:

```go
	expectedStream, err := normalizedRecordingStream(recordPlan, cfg.HUDMode, portraitSafeKillfeed)
	if err != nil {
		return fmt.Errorf("build recording profile: %w", err)
	}
```

Replace the CLI flag condition with:

```go
	if portraitSafeKillfeed {
		recorderArgs = append(recorderArgs, "--portrait-safe-killfeed")
	}
```

This intentionally lets Task 1 validation reject an impossible clean-HUD combination before the recorder launches rather than silently downgrading it.

- [ ] **Step 4: Run focused worker tests and the complete worker package**

Run:

```powershell
go test ./internal/workers -run 'TestRecordWorker(HUDFromPayloadOverridesDefault|InvalidatesGameplayCaptureWhenPortraitSafetyChanges)$' -count=1
go test ./internal/workers -count=1
```

Expected: PASS, including a two-run profile invalidation followed by compatible accumulation and idempotent skip.

- [ ] **Step 5: Record the review checkpoint without committing**

Run `git diff -- internal/workers/media_worker.go internal/workers/media_worker_test.go internal/workers/media_worker_segment_select_test.go` and verify that retry policy, queue behavior, and segment selection are unchanged.
Do not run `git commit` without separate user authorization.

---

### Task 5: Keep Native Full HUD Notices During Rendering

**Files:**
- Modify: `internal/editor/manifest.go:77-85`
- Test: `internal/editor/manifest_test.go:931-973`

**Interfaces:**
- Consumes: A validated persisted recording result with `Plan.Stream.PortraitSafeKillfeed`.
- Produces: `Manifest.KillfeedOverlay == false`, no `EffectKillfeed`, and no `kfsrc` FFmpeg branch for new native Full HUD recordings.
- Preserves: The overlay for legacy gameplay recordings whose flag is false and disables it for every landscape output.

- [ ] **Step 1: Expand the native portrait test to both HUD modes**

Replace `TestBuildManifestKeepsPortraitSafeDeathnoticesNative` with:

```go
func TestBuildManifestKeepsPortraitSafeKillfeedNative(t *testing.T) {
	for _, hudMode := range []recording.HUDMode{recording.HUDModeDeathnotices, recording.HUDModeGameplay} {
		t.Run(string(hudMode), func(t *testing.T) {
			dir := t.TempDir()
			result := testRecordingResult(dir)
			result.Plan.Stream.HUDMode = hudMode
			result.Plan.Stream.PortraitSafeKillfeed = true
			opts := testManifestOptions(dir, nil)
			opts.KillfeedOverlay = true

			manifest := mustBuildManifest(t, result, opts)
			if len(manifest.Warnings) != 0 {
				t.Fatalf("warnings = %v", manifest.Warnings)
			}
			short := manifest.Shorts[0]
			if short.KillfeedOverlay {
				t.Fatal("KillfeedOverlay = true, want native portrait-safe notices")
			}
			for _, effect := range short.Effects {
				if effect.Type == EffectKillfeed {
					t.Fatalf("unexpected frozen killfeed effect %#v", effect)
				}
			}
			if command := strings.Join(short.FFmpegCommand, " "); strings.Contains(command, "kfsrc") {
				t.Fatalf("command contains legacy killfeed crop branch: %s", command)
			}
		})
	}
}
```

Keep `TestBuildManifestAppliesRetentionOverlaysAndTailTrim` unchanged because its default gameplay recording has no portrait-safe flag and proves legacy fallback compatibility.

- [ ] **Step 2: Run native and legacy manifest tests and confirm the red state**

Run:

```powershell
go test ./internal/editor -run 'TestBuildManifest(KeepsPortraitSafeKillfeedNative|AppliesRetentionOverlaysAndTailTrim|LandscapeNeverDuplicatesNativeKillfeed)$' -count=1
```

Expected: FAIL only for the portrait-safe gameplay subtest because the current native check also requires `HUDModeDeathnotices`.

- [ ] **Step 3: Trust the validated portrait-safe recording contract**

Replace the native killfeed calculation with:

```go
	nativePortraitKillfeed := result.Plan.Stream.PortraitSafeKillfeed
	killfeedOverlay := opts.KillfeedOverlay && renderPreset.KillfeedSource && outputFormat == OutputFormatShort9x16 && !nativePortraitKillfeed
```

Update the adjacent comment so it refers to legacy vertical killfeed captures rather than only legacy deathnotice captures.

- [ ] **Step 4: Run focused and complete editor tests**

Run:

```powershell
go test ./internal/editor -run 'TestBuildManifest(KeepsPortraitSafeKillfeedNative|AppliesRetentionOverlaysAndTailTrim|LandscapeNeverDuplicatesNativeKillfeed)$' -count=1
go test ./internal/editor -count=1
```

Expected: PASS for both commands.

- [ ] **Step 5: Record the review checkpoint without committing**

Run `git diff -- internal/editor/manifest.go internal/editor/manifest_test.go` and verify that framing, effects, and legacy artifact behavior are otherwise unchanged.
Do not run `git commit` without separate user authorization.

---

### Task 6: Format And Verify The Integrated Fix

**Files:**
- Verify: All Go files modified in Tasks 1 through 5.
- Verify: `docs/superpowers/specs/2026-07-21-full-hud-native-killfeed-design.md`.
- Verify: `docs/superpowers/plans/2026-07-21-full-hud-native-killfeed.md`.

**Interfaces:**
- Consumes: All prior task deliverables.
- Produces: A formatted, test-passing worktree with evidence that the capture profile reaches HLAE and the render manifest omits the synthetic overlay.

- [ ] **Step 1: Format only the changed Go files**

Run:

```powershell
gofmt -w internal/recording/types.go internal/recording/types_test.go internal/recording/scriptgen.go internal/recording/scriptgen_test.go internal/httpapi/handlers.go internal/httpapi/handlers_test.go internal/httpapi/handlers_generate_test.go internal/workers/media_worker.go internal/workers/media_worker_test.go internal/workers/media_worker_segment_select_test.go internal/editor/manifest.go internal/editor/manifest_test.go
```

Expected: No output and no unrelated file changes.

- [ ] **Step 2: Run the affected packages together without cache reuse**

Run:

```powershell
go test ./internal/recording ./internal/httpapi ./internal/workers ./internal/editor -count=1
```

Expected: PASS for all four packages.

- [ ] **Step 3: Run the complete Go test suite**

Run:

```powershell
go test ./... -count=1 -timeout 3m
```

Expected: PASS with no HLAE, CS2, or long media process launched.

- [ ] **Step 4: Run the CI-equivalent Go and subprocess-security gate**

Run:

```powershell
& "C:\Program Files\Git\bin\bash.exe" scripts/go-gate.sh --no-format --build --security
```

Expected: Formatting check, tests, vet, build, `zv check`, static analysis when available, and security checks all pass.

- [ ] **Step 5: Inspect final evidence**

Run:

```powershell
git diff --check
git status --short
git diff -- internal/recording internal/httpapi internal/workers internal/editor docs/superpowers
```

Expected: No whitespace errors, only intended source/tests/spec/plan changes, and no generated media.
Do not run `git commit` or `git push` without separate user authorization.

- [ ] **Step 6: Report the media-validation boundary**

State that automated verification proves the exact HLAE script and render-manifest contracts.
State that replacing the already-rendered Nuke reel still requires a separately approved real recapture and render after updated binaries are built.
