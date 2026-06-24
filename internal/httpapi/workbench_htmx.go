package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/moments"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type workbenchOnboardingView struct {
	HasJobs bool
}

type workbenchRoster struct {
	Players []workbenchRosterPlayer `json:"players"`
}

type workbenchRosterPlayer struct {
	SteamID64 string `json:"steamid64"`
	Name      string `json:"name"`
	Team      string `json:"team"`
	Kills     int    `json:"kills"`
	Deaths    int    `json:"deaths"`
	Assists   int    `json:"assists"`
}

type workbenchJobView struct {
	Job             job.Job
	Loadouts        []renderplan.Loadout
	Songs           []song
	Moments         []moments.Moment
	MomentsError    string
	Roster          []workbenchRosterPlayer
	RosterError     string
	RenderState     *renderplan.RenderVariantState
	RenderError     string
	Variant         string
	DefaultEdit     renderplan.EditRequest
	CanParse        bool
	CanRecord       bool
	CanRender       bool
	CanCaptionAgent bool
	ArtifactLinks   []workbenchArtifactLink
}

type workbenchArtifactLink struct {
	Label string
	Href  string
	Ready bool
}

type bufferedResponse struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: http.Header{}}
}

func (w *bufferedResponse) Header() http.Header {
	return w.header
}

func (w *bufferedResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}

func (w *bufferedResponse) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *bufferedResponse) statusCode() int {
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}

// WorkbenchJobs renders the live run list for the HTMX workbench.
func (h *Handlers) WorkbenchJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.repo.List(r.Context(), 50)
	if err != nil {
		h.renderWorkbenchError(w, "list jobs", err)
		return
	}
	selected := h.workbenchSelectedJobID(r)
	if selected == "" && len(jobs) > 0 {
		selected = jobs[0].ID.String()
	}
	data := struct {
		Jobs     []job.Job
		Selected string
	}{Jobs: jobs, Selected: selected}
	renderWorkbenchTemplate(w, workbenchJobsTemplate, data)
}

// WorkbenchWorkspace renders the initial center panel. It selects the job from
// ?job= when HTMX loads after an upload redirect, otherwise the newest known
// job, and falls back to onboarding when there is no run yet.
func (h *Handlers) WorkbenchWorkspace(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.repo.List(r.Context(), 50)
	if err != nil {
		h.renderWorkbenchError(w, "list jobs", err)
		return
	}
	selected := h.workbenchSelectedJobID(r)
	if selected == "" && len(jobs) > 0 {
		selected = jobs[0].ID.String()
	}
	if selected == "" {
		renderWorkbenchTemplate(w, workbenchOnboardingTemplate, workbenchOnboardingView{})
		return
	}
	id, err := uuid.Parse(selected)
	if err != nil {
		renderWorkbenchTemplate(w, workbenchOnboardingTemplate, workbenchOnboardingView{HasJobs: len(jobs) > 0})
		return
	}
	view, err := h.workbenchJobView(r, id)
	if err != nil {
		h.renderWorkbenchError(w, "load job", err)
		return
	}
	renderWorkbenchTemplate(w, workbenchJobTemplate, view)
}

// WorkbenchJob renders one selected production run.
func (h *Handlers) WorkbenchJob(w http.ResponseWriter, r *http.Request) {
	id, ok := h.workbenchJobID(w, r)
	if !ok {
		return
	}
	view, err := h.workbenchJobView(r, id)
	if err != nil {
		h.renderWorkbenchError(w, "load job", err)
		return
	}
	renderWorkbenchTemplate(w, workbenchJobTemplate, view)
}

// WorkbenchCreateJob adapts the HTMX upload form to POST /api/jobs.
func (h *Handlers) WorkbenchCreateJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBytes)
	if err := r.ParseMultipartForm(multipartMemBudget); err != nil {
		h.renderWorkbenchError(w, "parse upload form", err)
		return
	}
	target := strings.TrimSpace(r.FormValue("target_steamid"))
	if target != "" {
		cfg, err := json.Marshal(createJobConfig{TargetSteamID: target})
		if err != nil {
			h.renderWorkbenchError(w, "build upload config", err)
			return
		}
		r.MultipartForm.Value["config"] = []string{string(cfg)}
		r.Form.Set("config", string(cfg))
		r.PostForm.Set("config", string(cfg))
	}

	resp := h.capture(r, h.CreateJob)
	if resp.statusCode() >= 400 {
		h.renderWorkbenchActionError(w, "create job", resp)
		return
	}
	var created struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(resp.body.Bytes(), &created); err != nil {
		h.renderWorkbenchError(w, "decode created job", err)
		return
	}
	w.Header().Set("HX-Redirect", "/?job="+created.ID.String())
	w.WriteHeader(http.StatusNoContent)
}

// WorkbenchStartParse adapts the roster picker to POST /api/jobs/{id}/parse.
func (h *Handlers) WorkbenchStartParse(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderWorkbenchError(w, "parse form", err)
		return
	}
	body, err := json.Marshal(startParseRequest{TargetSteamID: strings.TrimSpace(r.FormValue("target_steamid"))})
	if err != nil {
		h.renderWorkbenchError(w, "build parse request", err)
		return
	}
	setJSONBody(r, body)
	resp := h.capture(r, h.StartParse)
	if resp.statusCode() >= 400 {
		h.renderWorkbenchActionError(w, "start parse", resp)
		return
	}
	h.WorkbenchJob(w, r)
}

// WorkbenchStartRecording adapts the record approval form to POST
// /api/jobs/{id}/record.
func (h *Handlers) WorkbenchStartRecording(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderWorkbenchError(w, "parse form", err)
		return
	}
	body, err := json.Marshal(struct {
		Preset string `json:"preset"`
	}{Preset: strings.TrimSpace(r.FormValue("preset"))})
	if err != nil {
		h.renderWorkbenchError(w, "build record request", err)
		return
	}
	setJSONBody(r, body)
	resp := h.capture(r, h.StartRecording)
	if resp.statusCode() >= 400 {
		h.renderWorkbenchActionError(w, "start recording", resp)
		return
	}
	h.WorkbenchJob(w, r)
}

// WorkbenchStartRender adapts the render form to POST
// /api/jobs/{id}/renders/{variant}.
func (h *Handlers) WorkbenchStartRender(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderWorkbenchError(w, "parse form", err)
		return
	}
	variant := strings.TrimSpace(r.FormValue("variant"))
	if variant == "" {
		variant = editor.DefaultPreset().Name
	}
	chi.RouteContext(r.Context()).URLParams.Add("variant", variant)

	req := struct {
		Music string                 `json:"music,omitempty"`
		Edit  renderplan.EditRequest `json:"edit"`
	}{
		Music: strings.TrimSpace(r.FormValue("music")),
		Edit: renderplan.NormalizeEditRequest(renderplan.EditRequest{
			Format:     strings.TrimSpace(r.FormValue("format")),
			KillEffect: strings.TrimSpace(r.FormValue("kill_effect")),
			Transition: strings.TrimSpace(r.FormValue("transition")),
			Intro:      r.FormValue("intro") != "",
			Outro:      r.FormValue("outro") != "",
		}),
	}
	body, err := json.Marshal(req)
	if err != nil {
		h.renderWorkbenchError(w, "build render request", err)
		return
	}
	setJSONBody(r, body)
	resp := h.capture(r, h.StartRenderVariant)
	if resp.statusCode() >= 400 {
		h.renderWorkbenchActionError(w, "start render", resp)
		return
	}
	h.WorkbenchJob(w, r)
}

// WorkbenchStartCaptionAgent adapts the publish metadata button to the existing
// caption-agent endpoint.
func (h *Handlers) WorkbenchStartCaptionAgent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderWorkbenchError(w, "parse form", err)
		return
	}
	variant := strings.TrimSpace(r.FormValue("variant"))
	if variant == "" {
		variant = editor.DefaultPreset().Name
	}
	chi.RouteContext(r.Context()).URLParams.Add("variant", variant)
	resp := h.capture(r, h.StartCaptionAgent)
	if resp.statusCode() >= 400 {
		h.renderWorkbenchActionError(w, "start caption agent", resp)
		return
	}
	h.WorkbenchJob(w, r)
}

func (h *Handlers) capture(r *http.Request, handler func(http.ResponseWriter, *http.Request)) *bufferedResponse {
	resp := newBufferedResponse()
	handler(resp, r)
	return resp
}

func setJSONBody(r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
}

func (h *Handlers) workbenchJobID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "id")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handlers) workbenchSelectedJobID(r *http.Request) string {
	if selected := strings.TrimSpace(r.FormValue("selected")); selected != "" {
		return selected
	}
	current := strings.TrimSpace(r.Header.Get("HX-Current-URL"))
	if current == "" {
		current = r.URL.String()
	}
	u, err := url.Parse(current)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get("job"))
}

func (h *Handlers) workbenchJobView(r *http.Request, id uuid.UUID) (workbenchJobView, error) {
	j, err := h.repo.Get(r.Context(), id)
	if err != nil {
		return workbenchJobView{}, err
	}
	variant := strings.TrimSpace(r.FormValue("variant"))
	if variant == "" {
		variant = editor.DefaultPreset().Name
	}
	renderState, renderErr := h.workbenchRenderState(j.ID, variant)
	roster, rosterErr := h.workbenchRoster(j.ID)
	momentRows, momentsErr := h.workbenchMoments(j)
	view := workbenchJobView{
		Job:             j,
		Loadouts:        renderplan.LoadoutCatalog(),
		Songs:           h.workbenchSongs(),
		Moments:         momentRows,
		Variant:         variant,
		DefaultEdit:     renderplan.DefaultEditRequest(),
		CanParse:        j.Status == job.StatusScanned,
		CanRecord:       (j.Status == job.StatusParsed || j.Status == job.StatusFailed) && j.KillPlan != nil,
		CanRender:       j.Status == job.StatusRecorded || j.Status == job.StatusComposed || j.Status == job.StatusDone,
		CanCaptionAgent: renderState != nil && renderState.Status == renderplan.RenderVariantStatusReady,
		RenderState:     renderState,
		ArtifactLinks:   h.workbenchArtifactLinks(j, variant, renderState),
	}
	if rosterErr != nil {
		view.RosterError = rosterErr.Error()
	} else {
		view.Roster = roster
	}
	if momentsErr != nil {
		view.MomentsError = momentsErr.Error()
	}
	if renderErr != nil {
		view.RenderError = renderErr.Error()
	}
	return view, nil
}

func (h *Handlers) workbenchMoments(j job.Job) ([]moments.Moment, error) {
	if rc, err := h.storage.Open(moments.ArtifactKey(j.ID)); err == nil {
		defer rc.Close()
		var doc moments.Document
		if err := json.NewDecoder(rc).Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode moments: %w", err)
		}
		return doc.Moments, nil
	} else if !storage.IsNotExist(err) {
		return nil, err
	}
	if j.KillPlan == nil {
		return nil, fmt.Errorf("moments pending")
	}
	doc := moments.Build(j.ID, *j.KillPlan)
	return doc.Moments, nil
}

func (h *Handlers) workbenchRoster(id uuid.UUID) ([]workbenchRosterPlayer, error) {
	rc, err := h.storage.Open(artifacts.RosterKey(id))
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var roster workbenchRoster
	if err := json.NewDecoder(rc).Decode(&roster); err != nil {
		return nil, fmt.Errorf("decode roster: %w", err)
	}
	return roster.Players, nil
}

func (h *Handlers) workbenchRenderState(id uuid.UUID, variant string) (*renderplan.RenderVariantState, error) {
	if state, ok, err := h.readRenderVariantState(id, variant); err != nil || ok {
		return state, err
	}
	resultRef, err := renderplan.NewRenderVariantArtifactRef(id, variant, renderplan.RenderVariantArtifactResult, "")
	if err != nil {
		return nil, err
	}
	rc, err := h.storage.Open(resultRef.Key)
	if err != nil {
		if storage.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rc.Close()
	var result editor.Result
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode render result: %w", err)
	}
	status := renderplan.RenderVariantStatusReady
	if result.Error != "" {
		status = renderplan.RenderVariantStatusFailed
	}
	loadout, err := renderplan.LoadoutForVariant(variant)
	if err != nil {
		return nil, err
	}
	state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:    id,
		Loadout:  loadout,
		Status:   status,
		Warnings: result.Warnings,
		Error:    result.Error,
	})
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (h *Handlers) workbenchSongs() []song {
	if catalog, ok := h.loadMusicCatalog(); ok {
		out := make([]song, 0, len(catalog))
		for _, t := range catalog {
			if !songIDPattern.MatchString(t.ID) || h.resolveSongFile(t.ID) == "" {
				continue
			}
			title := t.Title
			if title == "" {
				title = humanizeSongID(t.ID)
			}
			out = append(out, song{
				ID:          t.ID,
				Title:       title,
				Artist:      t.Artist,
				Genre:       t.Genre,
				DurationSec: t.DurationSec,
				License:     t.License,
				AudioURL:    songAudioURL(t.ID),
			})
		}
		return out
	}
	return h.scanSongs()
}

func (h *Handlers) workbenchArtifactLinks(j job.Job, variant string, state *renderplan.RenderVariantState) []workbenchArtifactLink {
	id := j.ID.String()
	links := []workbenchArtifactLink{
		{Label: "kill_plan.json", Href: "/api/jobs/" + id + "/plan", Ready: j.KillPlan != nil || statusAtLeastPlan(j.Status)},
	}
	if state != nil {
		links = append(links,
			workbenchArtifactLink{Label: "edit-document.json", Href: "/api/jobs/" + id + "/renders/" + variant + "/edit-document", Ready: state.EditDocumentKey != ""},
			workbenchArtifactLink{Label: "pack-manifest.json", Href: "/api/jobs/" + id + "/renders/" + variant + "/pack", Ready: state.PackManifestKey != ""},
			workbenchArtifactLink{Label: "shortslistosparasubir", Href: "/api/jobs/" + id + "/renders/" + variant + "/gallery", Ready: state.GalleryKey != ""},
			workbenchArtifactLink{Label: "quality", Href: "/api/jobs/" + id + "/renders/" + variant + "/quality", Ready: state.Status == renderplan.RenderVariantStatusReady},
		)
	}
	return links
}

func statusAtLeastPlan(s job.Status) bool {
	switch s {
	case job.StatusParsed, job.StatusRecording, job.StatusRecorded, job.StatusComposing, job.StatusComposed, job.StatusDone:
		return true
	default:
		return false
	}
}

func (h *Handlers) renderWorkbenchError(w http.ResponseWriter, op string, err error) {
	renderWorkbenchTemplate(w, workbenchErrorTemplate, struct {
		Operation string
		Message   string
	}{Operation: op, Message: err.Error()})
}

func (h *Handlers) renderWorkbenchActionError(w http.ResponseWriter, op string, resp *bufferedResponse) {
	msg := strings.TrimSpace(resp.body.String())
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(resp.body.Bytes(), &body); err == nil && body.Error != "" {
		msg = body.Error
	}
	if msg == "" {
		msg = http.StatusText(resp.statusCode())
	}
	renderWorkbenchTemplate(w, workbenchErrorTemplate, struct {
		Operation string
		Message   string
	}{Operation: op, Message: msg})
}

func renderWorkbenchTemplate(w http.ResponseWriter, src string, data any) {
	tmpl := template.Must(template.New("workbench").Funcs(workbenchFuncs).Parse(src))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "render template", http.StatusInternalServerError)
	}
}

var workbenchFuncs = template.FuncMap{
	"shortID": func(id uuid.UUID) string {
		raw := id.String()
		if len(raw) < 8 {
			return raw
		}
		return raw[:8]
	},
	"fileName": func(path string) string {
		if path == "" {
			return "demo pending"
		}
		return filepath.Base(strings.ReplaceAll(path, "\\", "/"))
	},
	"progress": func(status job.Status) int {
		switch status {
		case job.StatusQueued:
			return 8
		case job.StatusScanning, job.StatusParsing:
			return 18
		case job.StatusScanned:
			return 26
		case job.StatusParsed:
			return 38
		case job.StatusRecording:
			return 56
		case job.StatusRecorded:
			return 68
		case job.StatusComposing:
			return 78
		case job.StatusComposed:
			return 88
		case job.StatusDone:
			return 100
		case job.StatusFailed:
			return 100
		default:
			return 0
		}
	},
	"checked": func(v bool) string {
		if v {
			return "checked"
		}
		return ""
	},
	"statusText": func(s job.Status) string { return s.String() },
	"renderStatus": func(s *renderplan.RenderVariantState) string {
		if s == nil || s.Status == "" {
			return "not started"
		}
		return s.Status
	},
	"formatSeconds": func(v float64) string {
		if v <= 0 {
			return "00:00.00"
		}
		minutes := int(v) / 60
		seconds := v - float64(minutes*60)
		return fmt.Sprintf("%02d:%05.2f", minutes, seconds)
	},
	"eventLabel": func(m moments.Moment) string {
		if len(m.ReasonCodes) > 0 {
			return strings.ReplaceAll(strings.Join(m.ReasonCodes, ", "), "_", " ")
		}
		if m.Events.Kills > 1 {
			return strconv.Itoa(m.Events.Kills) + "K highlight"
		}
		if m.Events.Utility > 0 {
			return "utility moment"
		}
		return "candidate moment"
	},
	"score":          func(v float64) string { return fmt.Sprintf("%.2f", v) },
	"taskTypeRender": func() string { return tasks.TypeRenderVariant },
}

const workbenchJobsTemplate = `
{{- if .Jobs -}}
  {{- range .Jobs }}
  <button
    type="button"
    class="run-item"
    aria-selected="{{ if eq $.Selected .ID.String }}true{{ else }}false{{ end }}"
    hx-get="/ui/jobs/{{ .ID }}"
    hx-target="#workspace"
    hx-swap="innerHTML"
    hx-on::after-request="document.getElementById('selected-job-id').value='{{ .ID }}'">
    <span class="run-line">
      <span class="run-title truncate">{{ fileName .DemoPath }}</span>
      <span class="status-badge {{ statusText .Status }}">{{ statusText .Status }}</span>
    </span>
    <span class="run-line">
      <span class="run-subtitle mono">{{ shortID .ID }}</span>
      <span class="run-subtitle truncate">{{ if .TargetSteamID }}{{ .TargetSteamID }}{{ else }}target pending{{ end }}</span>
    </span>
    <span class="progress"><span style="width: {{ progress .Status }}%"></span></span>
  </button>
  {{- end -}}
{{- else -}}
  <div class="queue-empty">
    <span class="meta-label">No runs yet</span>
    <p>Select a CS2 demo in Intake to start a local run.</p>
  </div>
{{- end -}}
`

const workbenchOnboardingTemplate = `
<div class="workspace-status">
  <span class="chip good">Ready for local run</span>
  <span class="chip">No Node server required</span>
  <span class="chip">HLAE capture stays on this PC</span>
</div>

<section class="onboarding-panel" aria-label="Local workflow onboarding">
  <div class="onboarding-copy">
    <span class="meta-label">Start here</span>
    <h2>Turn a CS2 demo into an upload-ready reel from this machine.</h2>
    <p>Use the Intake panel to upload a demo. Leave SteamID64 empty when you want FragForge to scan the roster first.</p>
  </div>
  <div class="onboarding-steps">
    <div class="onboarding-step active">
      <span class="step-index">1</span>
      <div><strong>Upload</strong><span>Pick a .dem and choose roster scan or direct target.</span></div>
    </div>
    <div class="onboarding-step">
      <span class="step-index">2</span>
      <div><strong>Parse</strong><span>Select the player and review detected moments.</span></div>
    </div>
    <div class="onboarding-step">
      <span class="step-index">3</span>
      <div><strong>Record</strong><span>Approve HLAE/CS2 capture when the plan is ready.</span></div>
    </div>
    <div class="onboarding-step">
      <span class="step-index">4</span>
      <div><strong>Render</strong><span>Choose format, effects, transition, music, intro and outro.</span></div>
    </div>
  </div>
</section>

<section class="onboarding-grid" aria-label="Local readiness">
  <article>
    <span class="meta-label">Capture</span>
    <strong>HLAE + CS2 local</strong>
    <p>Recording runs only after explicit approval from a parsed job.</p>
  </article>
  <article>
    <span class="meta-label">Output</span>
    <strong>Short or 16:9</strong>
    <p>Render controls are captured into the edit document for reproducibility.</p>
  </article>
  <article>
    <span class="meta-label">Publish</span>
    <strong>shortslistosparasubir</strong>
    <p>Ready artifacts appear as links as soon as render completes.</p>
  </article>
</section>
`

const workbenchJobTemplate = `
<input id="selected-job-id" type="hidden" name="selected" value="{{ .Job.ID }}" hx-swap-oob="true">

<div class="workspace-status">
  <span class="chip mono">{{ fileName .Job.DemoPath }}</span>
  <span class="status-badge {{ statusText .Job.Status }}">{{ statusText .Job.Status }}</span>
  <span class="chip mono">{{ shortID .Job.ID }}</span>
  <span class="chip">Render: {{ renderStatus .RenderState }}</span>
  {{ if .Job.FailureReason }}<span class="chip bad">{{ .Job.FailureReason }}</span>{{ end }}
</div>

<section class="preview-panel" aria-label="Selected run">
  <div class="preview-well">
    <div class="preview-empty">
      <span>{{ if .Job.KillPlan }}{{ .Job.KillPlan.Demo.Map }}{{ else }}Run {{ shortID .Job.ID }}{{ end }}</span>
      <span>{{ if .Job.TargetSteamID }}Target {{ .Job.TargetSteamID }}{{ else }}Waiting for roster pick{{ end }}</span>
    </div>
    <div class="preview-controls">
      <div class="control-group">
        <span class="chip">Preset {{ .Variant }}</span>
        <span class="chip">Task {{ taskTypeRender }}</span>
      </div>
      <span class="mono">{{ if .Moments }}{{ formatSeconds (index .Moments 0).TimeStart }} / {{ formatSeconds (index .Moments 0).TimeEnd }}{{ else }}00:00.00 / 00:00.00{{ end }}</span>
    </div>
  </div>
</section>

<section class="timeline-panel" aria-label="Actions">
  <div class="next-action">
    <span class="meta-label">Next action</span>
    {{ if .CanParse }}
      <strong>Choose the player to parse</strong>
      <p>The roster scan is ready. Pick the POV target and FragForge will build the kill plan.</p>
    {{ else if .CanRecord }}
      <strong>Approve local capture</strong>
      <p>The kill plan is ready. Recording will launch CS2 through HLAE on this PC.</p>
    {{ else if .CanCaptionAgent }}
      <strong>Prepare publish metadata</strong>
      <p>The render is ready. Generate captions and titles when you want publish copy.</p>
    {{ else if .CanRender }}
      <strong>Render the upload pack</strong>
      <p>Choose the delivery format and edit treatment, then create the upload-ready assets.</p>
    {{ else if eq (statusText .Job.Status) "failed" }}
      <strong>Inspect the failure</strong>
      <p>Use the status message above and retry the matching stage when the input is fixed.</p>
    {{ else }}
      <strong>Waiting for worker progress</strong>
      <p>The workbench refreshes the queue automatically while this job advances.</p>
    {{ end }}
  </div>
  <div class="htmx-actions">
    {{ if .CanParse }}
      <form class="inline-form" hx-post="/ui/jobs/{{ .Job.ID }}/parse" hx-target="#workspace" hx-swap="innerHTML">
        <select name="target_steamid" aria-label="Player">
          {{ range .Roster }}
            <option value="{{ .SteamID64 }}">{{ .Name }} · {{ .Team }} · {{ .Kills }}K/{{ .Deaths }}D</option>
          {{ end }}
        </select>
        <button class="button primary" type="submit">Parse Player</button>
      </form>
      {{ if .RosterError }}<span class="chip bad">{{ .RosterError }}</span>{{ end }}
    {{ end }}

    {{ if .CanRecord }}
      <form class="inline-form" hx-post="/ui/jobs/{{ .Job.ID }}/record" hx-target="#workspace" hx-swap="innerHTML">
        <select name="preset" aria-label="Recording preset">
          {{ range .Loadouts }}
                    <option value="{{ .Variant }}" {{ if eq $.Variant .Variant }}selected{{ end }}>{{ .Variant }}</option>
          {{ end }}
        </select>
        <button class="button primary" type="submit">Record With HLAE</button>
      </form>
    {{ end }}

    {{ if .CanRender }}
      <form class="render-form" hx-post="/ui/jobs/{{ .Job.ID }}/render" hx-target="#workspace" hx-swap="innerHTML">
        <label>
          <span>Preset</span>
          <select name="variant">
            {{ range .Loadouts }}
              <option value="{{ .Variant }}" {{ if eq $.Variant .Variant }}selected{{ end }}>{{ .Variant }}</option>
            {{ end }}
          </select>
        </label>
        <label>
          <span>Music</span>
          <select name="music">
            <option value="">No music</option>
            {{ range .Songs }}
              <option value="{{ .ID }}">{{ .Title }}</option>
            {{ end }}
          </select>
        </label>
        <label>
          <span>Format</span>
          <select name="format">
            <option value="short-9x16" {{ if eq .DefaultEdit.Format "short-9x16" }}selected{{ end }}>Short 9:16</option>
            <option value="landscape-16x9">16:9</option>
          </select>
        </label>
        <label>
          <span>Kill effect</span>
          <select name="kill_effect">
            <option value="clean">Clean</option>
            <option value="punch-in" {{ if eq .DefaultEdit.KillEffect "punch-in" }}selected{{ end }}>Punch-in</option>
            <option value="velocity">Velocity</option>
            <option value="freeze-flash">Freeze flash</option>
          </select>
        </label>
        <label>
          <span>Transition</span>
          <select name="transition">
            <option value="cut">Cut</option>
            <option value="flash" {{ if eq .DefaultEdit.Transition "flash" }}selected{{ end }}>Flash</option>
            <option value="whip">Whip</option>
            <option value="dip">Dip</option>
          </select>
        </label>
        <label class="check-row"><input name="intro" type="checkbox"> Intro</label>
        <label class="check-row"><input name="outro" type="checkbox"> Outro</label>
        <button class="button primary" type="submit">Render Upload Pack</button>
      </form>
    {{ end }}

    {{ if .CanCaptionAgent }}
      <form class="inline-form" hx-post="/ui/jobs/{{ .Job.ID }}/agent/captions" hx-target="#workspace" hx-swap="innerHTML">
        <input type="hidden" name="variant" value="{{ .Variant }}">
        <button class="button secondary" type="submit">Generate Captions</button>
      </form>
    {{ end }}
  </div>
</section>

<section class="moments-panel" aria-label="Detected moments">
  <div class="panel-head compact">
    <h2>Detected Moments</h2>
    <span class="meta-label">{{ len .Moments }} items</span>
  </div>
  <div class="table-wrap">
    {{ if .MomentsError }}
      <div class="empty-state">{{ .MomentsError }}</div>
    {{ else if .Moments }}
      <table>
        <thead>
          <tr><th>Time</th><th>Event</th><th>Player/POV</th><th>Score</th></tr>
        </thead>
        <tbody>
          {{ range .Moments }}
            <tr>
              <td class="mono">{{ formatSeconds .TimeStart }}</td>
              <td>{{ eventLabel . }}</td>
              <td>{{ .Player }}</td>
              <td class="score-good">{{ score .Score }}</td>
            </tr>
          {{ end }}
        </tbody>
      </table>
    {{ else }}
      <div class="empty-state">No moments detected yet.</div>
    {{ end }}
  </div>
</section>

<section class="artifact-strip" aria-label="Artifacts">
  <span class="meta-label">Artifacts</span>
  <div class="artifact-links">
    {{ range .ArtifactLinks }}
      {{ if .Ready }}
        <a class="artifact ready" href="{{ .Href }}" target="_blank" rel="noreferrer">{{ .Label }}</a>
      {{ else }}
        <span class="artifact pending">{{ .Label }}</span>
      {{ end }}
    {{ end }}
    {{ if .RenderError }}<span class="artifact pending">{{ .RenderError }}</span>{{ end }}
  </div>
</section>
`

const workbenchErrorTemplate = `
<div class="workspace-status">
  <span class="chip bad">{{ .Operation }}</span>
  <span class="chip">{{ .Message }}</span>
</div>
<section class="preview-panel">
  <div class="empty-state">{{ .Message }}</div>
</section>
`
