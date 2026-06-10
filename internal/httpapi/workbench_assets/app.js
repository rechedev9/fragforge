"use strict";
const state = {
    jobs: [],
    loadouts: [],
    selectedJobId: null,
    selectedVariant: "viral-60",
    moments: [],
    selectedMomentIds: new Set(),
    renderState: null,
    activeTab: "codex",
    lastMomentsError: "",
    lastRenderError: "",
    commandHistory: [],
};
let selectedDemoFile = null;
let refreshInFlight = false;
let refreshTimer;
const stages = ["Intake", "Parse", "Plan", "Record", "Render", "Validate", "Publish"];
function required(id) {
    const node = document.getElementById(id);
    if (!node) {
        throw new Error(`missing element #${id}`);
    }
    return node;
}
const dom = {
    apiChip: required("api-chip"),
    artifactLinks: required("artifact-links"),
    actionProposal: required("action-proposal"),
    codexCommand: required("codex-command"),
    codexThread: required("codex-thread"),
    commandForm: required("command-form"),
    demoFile: required("demo-file"),
    demoFileName: required("demo-file-name"),
    dropZone: required("drop-zone"),
    health: required("health"),
    inspectorJson: required("inspector-json"),
    jobCount: required("job-count"),
    jobs: required("jobs"),
    momentCount: required("moment-count"),
    moments: required("moments"),
    previewSubtitle: required("preview-subtitle"),
    previewTimecode: required("preview-timecode"),
    previewTitle: required("preview-title"),
    railArtifacts: required("rail-artifacts"),
    refresh: required("refresh"),
    runPrompt: required("run-prompt"),
    stageTimeline: required("stage-timeline"),
    startRun: required("start-run"),
    targetSteamID: required("target-steamid"),
    token: required("token"),
    workspaceStatus: required("workspace-status"),
};
function text(tag, className, value) {
    const node = document.createElement(tag);
    if (className) {
        node.className = className;
    }
    node.textContent = value;
    return node;
}
function setHealth(message, kind = "ok") {
    dom.health.textContent = message;
    dom.apiChip.textContent = kind === "bad" ? "API: error" : "API: ready";
    dom.apiChip.className = kind === "bad" ? "chip bad" : "chip good";
}
function errorMessage(err) {
    return err instanceof Error ? err.message : String(err);
}
async function api(path, init) {
    const res = await fetch(path, init);
    const raw = await res.text();
    let body = {};
    if (raw) {
        try {
            body = JSON.parse(raw);
        }
        catch {
            body = { text: raw };
        }
    }
    if (!res.ok) {
        const errBody = body;
        throw new Error(errBody.error || errBody.text || res.statusText);
    }
    return body;
}
function mutationHeaders(json = false) {
    const headers = new Headers();
    const token = dom.token.value.trim();
    if (token) {
        headers.set("X-FragForge-Token", token);
        localStorage.setItem("zv_mutation_token", token);
    }
    if (json) {
        headers.set("Content-Type", "application/json");
    }
    return headers;
}
function selectedJob() {
    if (!state.selectedJobId) {
        return null;
    }
    return state.jobs.find((job) => job.id === state.selectedJobId) || null;
}
function selectedVariant() {
    if (state.selectedVariant) {
        return state.selectedVariant;
    }
    const first = state.loadouts[0]?.variant;
    return first || "viral-60";
}
function shortID(id) {
    return id ? id.slice(0, 8) : "--------";
}
function fileName(path) {
    if (!path) {
        return "no demo";
    }
    const normalized = path.replace(/\\/g, "/");
    return normalized.slice(normalized.lastIndexOf("/") + 1);
}
function setSelectedDemoFile(file) {
    selectedDemoFile = file;
    dom.demoFileName.textContent = file?.name || "No file selected";
}
function statusProgress(status) {
    switch (status) {
        case "queued":
            return 8;
        case "parsing":
            return 18;
        case "parsed":
            return 38;
        case "recording":
            return 56;
        case "recorded":
            return 68;
        case "composing":
            return 78;
        case "composed":
            return 88;
        case "done":
            return 100;
        case "failed":
            return 100;
        default:
            return 0;
    }
}
function shouldAutoRefresh() {
    const job = selectedJob();
    if (!job) {
        return false;
    }
    if (["queued", "parsing", "recording", "composing"].includes(job.status)) {
        return true;
    }
    return ["queued", "rendering"].includes(state.renderState?.status || "");
}
function scheduleRefresh() {
    if (refreshTimer !== undefined) {
        window.clearTimeout(refreshTimer);
        refreshTimer = undefined;
    }
    if (!shouldAutoRefresh()) {
        return;
    }
    refreshTimer = window.setTimeout(() => {
        refreshTimer = undefined;
        void refresh({ silent: true });
    }, 3000);
}
function statusBadge(status) {
    const badge = text("span", `status-badge ${status || ""}`, status || "unknown");
    return badge;
}
function formatSeconds(value) {
    if (!Number.isFinite(value || 0) || !value) {
        return "00:00.00";
    }
    const minutes = Math.floor(value / 60);
    const seconds = value - minutes * 60;
    return `${String(minutes).padStart(2, "0")}:${seconds.toFixed(2).padStart(5, "0")}`;
}
function formatScore(score) {
    if (typeof score !== "number") {
        return "0.00";
    }
    return score.toFixed(2);
}
function momentEventLabel(moment) {
    const reasons = moment.reason_codes || [];
    if (reasons.length > 0) {
        return reasons.slice(0, 2).map((reason) => reason.replace(/_/g, " ")).join(", ");
    }
    const kills = moment.events?.kills || 0;
    if (kills > 1) {
        return `${kills}K highlight`;
    }
    if ((moment.events?.utility || 0) > 0) {
        return "utility moment";
    }
    return "candidate moment";
}
function renderStatusChips(job) {
    dom.workspaceStatus.replaceChildren();
    if (!job) {
        dom.workspaceStatus.append(text("span", "chip", "No run selected"), text("span", "chip", "Preset: viral-60"), text("span", "chip good", "OK"));
        return;
    }
    const variantSelect = document.createElement("select");
    variantSelect.setAttribute("aria-label", "Render preset");
    for (const loadout of state.loadouts) {
        const option = document.createElement("option");
        option.value = loadout.variant;
        option.textContent = loadout.variant;
        variantSelect.append(option);
    }
    variantSelect.value = selectedVariant();
    variantSelect.addEventListener("change", async () => {
        state.selectedVariant = variantSelect.value;
        await loadSelectedDetails();
        renderAll();
    });
    dom.workspaceStatus.append(text("span", "chip mono", fileName(job.demo_path)), statusBadge(job.status), text("span", "chip mono", elapsedLabel(job)), variantSelect, text("span", "chip good", "OK"));
}
function elapsedLabel(job) {
    const started = Date.parse(job.created_at || "");
    if (!Number.isFinite(started)) {
        return "04:12";
    }
    const seconds = Math.max(0, Math.floor((Date.now() - started) / 1000));
    const minutes = Math.floor(seconds / 60);
    return `${String(minutes).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}
function renderJobs() {
    dom.jobCount.textContent = `${state.jobs.length} total`;
    dom.jobs.replaceChildren();
    if (state.jobs.length === 0) {
        dom.jobs.append(text("div", "empty-state", "No active jobs."));
        return;
    }
    for (const job of state.jobs) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "run-item";
        button.dataset.id = job.id;
        button.setAttribute("aria-selected", String(job.id === state.selectedJobId));
        const topLine = document.createElement("div");
        topLine.className = "run-line";
        const title = text("span", "run-title truncate", fileName(job.demo_path));
        topLine.append(title, statusBadge(job.status));
        const middleLine = document.createElement("div");
        middleLine.className = "run-line";
        middleLine.append(text("span", "run-subtitle mono", shortID(job.id)), text("span", "run-subtitle truncate", job.target_steamid || "target pending"));
        const progress = document.createElement("div");
        progress.className = "progress";
        const bar = document.createElement("span");
        bar.style.width = `${statusProgress(job.status)}%`;
        progress.append(bar);
        button.append(topLine, middleLine, progress);
        button.addEventListener("click", async () => {
            state.selectedJobId = job.id;
            await loadSelectedDetails();
            renderAll();
        });
        dom.jobs.append(button);
    }
}
function stageState(stage, job) {
    if (!job) {
        return stage === "Intake" ? "active" : "pending";
    }
    const status = job.status;
    const renderStatus = state.renderState?.status;
    const done = (names) => names.includes(stage);
    if (status === "failed") {
        return done(["Intake", "Parse"]) ? "done" : stage === "Validate" ? "active" : "pending";
    }
    if (status === "queued") {
        return stage === "Intake" ? "active" : "pending";
    }
    if (status === "parsing") {
        return stage === "Parse" ? "active" : stage === "Intake" ? "done" : "pending";
    }
    if (status === "parsed") {
        return done(["Intake", "Parse", "Plan"]) ? "done" : stage === "Record" ? "active" : "pending";
    }
    if (status === "recording") {
        return done(["Intake", "Parse", "Plan"]) ? "done" : stage === "Record" ? "active" : "pending";
    }
    if (status === "recorded") {
        if (renderStatus === "queued" || renderStatus === "rendering") {
            return done(["Intake", "Parse", "Plan", "Record"]) ? "done" : stage === "Render" ? "active" : "pending";
        }
        if (renderStatus === "ready") {
            return done(["Intake", "Parse", "Plan", "Record", "Render"]) ? "done" : stage === "Validate" ? "active" : "pending";
        }
        return done(["Intake", "Parse", "Plan", "Record"]) ? "done" : stage === "Render" ? "active" : "pending";
    }
    if (status === "composing") {
        return done(["Intake", "Parse", "Plan", "Record"]) ? "done" : stage === "Render" ? "active" : "pending";
    }
    if (status === "composed") {
        return done(["Intake", "Parse", "Plan", "Record", "Render", "Validate"]) ? "done" : stage === "Publish" ? "active" : "pending";
    }
    if (status === "done") {
        return "done";
    }
    return "pending";
}
function renderTimeline(job) {
    dom.stageTimeline.replaceChildren();
    for (const stage of stages) {
        const item = document.createElement("div");
        item.className = `stage ${stageState(stage, job)}`;
        item.append(text("span", "stage-node", ""), text("span", "", stage));
        dom.stageTimeline.append(item);
    }
}
function renderPreview(job) {
    if (!job) {
        dom.previewTitle.textContent = "No job selected";
        dom.previewSubtitle.textContent = "Choose a run from the queue or create one from Intake.";
        dom.previewTimecode.textContent = "00:00.00 / 00:00.00";
        return;
    }
    const first = state.moments[0];
    dom.previewTitle.textContent = `${fileName(job.demo_path)} review`;
    dom.previewSubtitle.textContent = state.renderState?.status === "ready"
        ? "Render ready for validation and publish review."
        : `Current stage: ${job.status}`;
    dom.previewTimecode.textContent = first
        ? `${formatSeconds(first.time_start_seconds)} / ${formatSeconds(first.time_end_seconds)}`
        : "00:00.00 / 35:00.00";
}
function renderMoments(job) {
    dom.momentCount.textContent = `${state.moments.length} items`;
    dom.moments.replaceChildren();
    if (!job) {
        dom.moments.append(text("div", "empty-state", "No job selected."));
        return;
    }
    if (state.lastMomentsError) {
        dom.moments.append(text("div", "empty-state", state.lastMomentsError));
        return;
    }
    if (state.moments.length === 0) {
        dom.moments.append(text("div", "empty-state", "No moments detected yet."));
        return;
    }
    const table = document.createElement("table");
    const thead = document.createElement("thead");
    const headRow = document.createElement("tr");
    for (const label of ["Time", "Event", "Player/POV", "Score", "Selected"]) {
        headRow.append(text("th", "", label));
    }
    thead.append(headRow);
    const tbody = document.createElement("tbody");
    for (const moment of state.moments.slice(0, 40)) {
        const row = document.createElement("tr");
        row.append(text("td", "mono", formatSeconds(moment.time_start_seconds)), text("td", "", momentEventLabel(moment)), text("td", "", moment.player || "unknown"), text("td", (moment.score || 0) >= 0.9 ? "score-good" : "", formatScore(moment.score)));
        const selected = document.createElement("td");
        const checkbox = document.createElement("input");
        checkbox.type = "checkbox";
        checkbox.checked = state.selectedMomentIds.has(moment.id);
        checkbox.addEventListener("change", () => {
            if (checkbox.checked) {
                state.selectedMomentIds.add(moment.id);
            }
            else {
                state.selectedMomentIds.delete(moment.id);
            }
            renderInspector();
        });
        selected.append(checkbox);
        row.append(selected);
        tbody.append(row);
    }
    table.append(thead, tbody);
    dom.moments.append(table);
}
function artifactLinks(job) {
    if (!job) {
        return [];
    }
    const id = job.id;
    const variant = selectedVariant();
    const render = state.renderState;
    return [
        { label: "kill_plan.json", shortLabel: "kill_plan", href: `/api/jobs/${id}/plan`, ready: Boolean(job.kill_plan) || ["parsed", "recorded", "composed", "done"].includes(job.status) },
        { label: "manifest.json", shortLabel: "manifest", href: render?.pack_manifest_key ? `/api/jobs/${id}/renders/${variant}/pack` : undefined, ready: Boolean(render?.pack_manifest_key) },
        { label: "shortslistosparasubir", shortLabel: "upload-ready", href: render?.gallery_key ? `/api/jobs/${id}/renders/${variant}/gallery` : undefined, ready: render?.status === "ready" },
        { label: "validation", shortLabel: "validation", href: `/api/jobs/${id}/renders/${variant}/quality`, ready: render?.status === "ready" },
        { label: "caption agent", shortLabel: "captions", href: `/api/jobs/${id}/renders/${variant}/agent/captions`, ready: Boolean(render?.publish_summary_key) },
    ];
}
function renderArtifacts(job) {
    const links = artifactLinks(job);
    dom.artifactLinks.replaceChildren();
    dom.railArtifacts.replaceChildren();
    if (!job) {
        dom.artifactLinks.append(text("span", "artifact pending", "no run selected"));
        dom.railArtifacts.append(text("div", "empty-state", "Select a run to inspect artifacts."));
        return;
    }
    for (const link of links) {
        const chip = document.createElement(link.href ? "a" : "span");
        chip.className = `artifact ${link.ready ? "ready" : "pending"}`;
        chip.textContent = link.shortLabel;
        chip.title = link.label;
        if (link.href) {
            chip.href = link.href;
        }
        dom.artifactLinks.append(chip);
        const row = document.createElement("div");
        row.className = "artifact-row";
        row.append(text("span", "", link.label), text("span", link.ready ? "status-badge ready" : "status-badge queued", link.ready ? "ready" : "pending"));
        if (link.href) {
            const open = document.createElement("a");
            open.href = link.href;
            open.textContent = "Open";
            row.append(open);
        }
        dom.railArtifacts.append(row);
    }
}
function currentProposal(job) {
    if (!job) {
        return {
            code: "START_NEW_RUN",
            description: "Drop a demo, provide the target SteamID64, and start parsing.",
            approveLabel: "Waiting",
            disabled: true,
        };
    }
    if (job.status === "parsed") {
        return {
            code: "APPROVE_RECORDING",
            description: "Records the parsed plan with HLAE/CS2 after operator approval.",
            approveLabel: "Approve",
            disabled: false,
            action: () => postAction(`/api/jobs/${job.id}/record`),
        };
    }
    if (job.status === "recorded" || job.status === "composed" || job.status === "done") {
        if (state.renderState?.status !== "ready") {
            return {
                code: "RENDER_VARIANT",
                description: `Render ${selectedVariant()} and prepare upload-ready assets.`,
                approveLabel: "Render",
                disabled: false,
                action: () => postAction(`/api/jobs/${job.id}/renders/${selectedVariant()}`),
            };
        }
        return {
            code: "CAPTION_AGENT",
            description: "Generate captions and publish metadata for the ready render pack.",
            approveLabel: "Captions",
            disabled: false,
            action: () => postAction(`/api/jobs/${job.id}/renders/${selectedVariant()}/agent/captions`),
        };
    }
    if (job.status === "failed") {
        return {
            code: "INSPECT_FAILURE",
            description: job.failure_reason || "The job failed. Open Inspector for raw state and retry from the matching stage.",
            approveLabel: "Inspect",
            disabled: false,
            action: async () => setTab("inspector"),
        };
    }
    return {
        code: "WAIT_FOR_WORKER",
        description: `Current job status is ${job.status}. The workbench is polling for worker updates.`,
        approveLabel: "Refresh",
        disabled: false,
        action: refresh,
    };
}
function renderCodex(job) {
    dom.codexThread.replaceChildren();
    const system = message("codex", "Codex Operator", "Welcome. The workbench is initialized and ready for production.");
    const prompt = dom.runPrompt.value.trim();
    const userCopy = prompt || "Haz un Short con las mejores jugadas de martinez.";
    const user = message("user", "Operator", userCopy);
    const statusCopy = job
        ? `Selected ${fileName(job.demo_path)}. Stage ${job.status}. ${state.moments.length} moments loaded.`
        : "No run selected. Create or select a run to review moments and approve actions.";
    const codex = message("codex", "Codex Operator", statusCopy);
    dom.codexThread.append(system, user, codex);
    const proposal = currentProposal(job);
    dom.actionProposal.replaceChildren();
    dom.actionProposal.append(text("div", "proposal-code mono", proposal.code), text("p", "proposal-copy", proposal.description));
    const actions = document.createElement("div");
    actions.className = "proposal-actions";
    const approve = document.createElement("button");
    approve.type = "button";
    approve.className = "button primary";
    approve.textContent = proposal.approveLabel;
    approve.disabled = proposal.disabled;
    approve.addEventListener("click", async () => {
        if (!proposal.action) {
            return;
        }
        await proposal.action();
    });
    const reject = document.createElement("button");
    reject.type = "button";
    reject.className = "button secondary";
    reject.textContent = "Reject";
    reject.addEventListener("click", () => setHealth("proposal rejected", "warn"));
    const details = document.createElement("button");
    details.type = "button";
    details.className = "button secondary";
    details.textContent = "Show details";
    details.addEventListener("click", () => setTab("inspector"));
    actions.append(approve, reject, details);
    dom.actionProposal.append(actions);
}
function message(kind, title, copy) {
    const node = document.createElement("article");
    node.className = `message ${kind === "user" ? "user" : ""}`;
    const head = document.createElement("div");
    head.className = "message-head";
    head.append(text("span", "", title), text("span", "", kind === "user" ? "Operator" : "System"));
    node.append(head, text("p", "", copy));
    return node;
}
function renderInspector() {
    const job = selectedJob();
    dom.inspectorJson.textContent = JSON.stringify({
        job,
        render: state.renderState,
        selected_variant: selectedVariant(),
        selected_moment_ids: Array.from(state.selectedMomentIds),
        moments_error: state.lastMomentsError || undefined,
        render_error: state.lastRenderError || undefined,
        command_history: state.commandHistory,
    }, null, 2);
}
function renderTabs() {
    document.querySelectorAll("[data-tab]").forEach((tab) => {
        const isActive = tab.dataset.tab === state.activeTab;
        tab.classList.toggle("active", isActive);
    });
    for (const tab of ["codex", "inspector", "artifacts"]) {
        required(`tab-${tab}`).classList.toggle("active", tab === state.activeTab);
    }
}
function renderAll() {
    const job = selectedJob();
    renderStatusChips(job);
    renderJobs();
    renderTimeline(job);
    renderPreview(job);
    renderMoments(job);
    renderArtifacts(job);
    renderCodex(job);
    renderInspector();
    renderTabs();
}
async function refresh(options = {}) {
    if (refreshInFlight) {
        return;
    }
    refreshInFlight = true;
    if (!options.silent) {
        setHealth("loading");
    }
    try {
        const previousSelectedJobId = state.selectedJobId;
        const [jobsResponse, loadoutsResponse] = await Promise.all([
            api("/api/jobs?limit=50"),
            api("/api/loadouts"),
        ]);
        state.jobs = jobsResponse.jobs || [];
        state.loadouts = loadoutsResponse.loadouts || [];
        if (!state.loadouts.some((loadout) => loadout.variant === state.selectedVariant)) {
            state.selectedVariant = state.loadouts[0]?.variant || "viral-60";
        }
        if (!state.selectedJobId || !state.jobs.some((job) => job.id === state.selectedJobId)) {
            state.selectedJobId = state.jobs[0]?.id || null;
        }
        if (state.selectedJobId !== previousSelectedJobId) {
            state.selectedMomentIds.clear();
        }
        await loadSelectedDetails();
        renderAll();
        setHealth(shouldAutoRefresh() ? "watching" : "ready");
    }
    catch (err) {
        setHealth(errorMessage(err), "bad");
        renderAll();
    }
    finally {
        refreshInFlight = false;
        scheduleRefresh();
    }
}
async function loadSelectedDetails() {
    const job = selectedJob();
    state.moments = [];
    state.renderState = null;
    state.lastMomentsError = "";
    state.lastRenderError = "";
    if (!job) {
        state.selectedMomentIds.clear();
        return;
    }
    const [momentsResult, renderResult] = await Promise.allSettled([
        api(`/api/jobs/${job.id}/moments`),
        api(`/api/jobs/${job.id}/renders/${selectedVariant()}`),
    ]);
    if (momentsResult.status === "fulfilled") {
        state.moments = momentsResult.value.moments || [];
        if (state.selectedMomentIds.size === 0) {
            for (const moment of state.moments) {
                state.selectedMomentIds.add(moment.id);
            }
        }
    }
    else {
        state.lastMomentsError = momentsResult.reason instanceof Error ? momentsResult.reason.message : String(momentsResult.reason);
        state.selectedMomentIds.clear();
    }
    if (renderResult.status === "fulfilled") {
        state.renderState = renderResult.value;
    }
    else {
        state.lastRenderError = renderResult.reason instanceof Error ? renderResult.reason.message : String(renderResult.reason);
    }
}
async function createRun() {
    const file = selectedDemoFile || dom.demoFile.files?.[0];
    const targetSteamID = dom.targetSteamID.value.trim();
    if (!file) {
        setHealth("choose a .dem file", "warn");
        return;
    }
    if (!/^\d{16,20}$/.test(targetSteamID)) {
        setHealth("target SteamID64 required", "warn");
        return;
    }
    const form = new FormData();
    form.append("demo", file);
    form.append("config", JSON.stringify({ target_steamid: targetSteamID }));
    setHealth("creating");
    try {
        const headers = mutationHeaders(false);
        const init = { method: "POST", body: form };
        if (Array.from(headers.keys()).length > 0) {
            init.headers = headers;
        }
        const created = await api("/api/jobs", init);
        state.selectedJobId = created.id;
        state.selectedMomentIds.clear();
        await refresh();
    }
    catch (err) {
        setHealth(errorMessage(err), "bad");
    }
}
async function postAction(path, body) {
    setHealth("working");
    try {
        const init = { method: "POST" };
        if (body !== undefined) {
            init.body = JSON.stringify(body);
            init.headers = mutationHeaders(true);
        }
        else {
            const headers = mutationHeaders(false);
            if (Array.from(headers.keys()).length > 0) {
                init.headers = headers;
            }
        }
        await api(path, init);
        await refresh();
    }
    catch (err) {
        setHealth(errorMessage(err), "bad");
    }
}
function setTab(tab) {
    state.activeTab = tab;
    renderTabs();
}
function wireEvents() {
    dom.token.value = localStorage.getItem("zv_mutation_token") || "";
    dom.refresh.addEventListener("click", () => {
        void refresh();
    });
    dom.startRun.addEventListener("click", () => {
        void createRun();
    });
    dom.demoFile.addEventListener("change", () => {
        setSelectedDemoFile(dom.demoFile.files?.[0] || null);
    });
    dom.dropZone.addEventListener("dragover", (event) => {
        event.preventDefault();
        dom.dropZone.classList.add("dragging");
    });
    dom.dropZone.addEventListener("dragleave", () => {
        dom.dropZone.classList.remove("dragging");
    });
    dom.dropZone.addEventListener("drop", (event) => {
        event.preventDefault();
        dom.dropZone.classList.remove("dragging");
        if (event.dataTransfer?.files?.length) {
            setSelectedDemoFile(event.dataTransfer.files[0] || null);
        }
    });
    document.querySelectorAll("[data-tab]").forEach((button) => {
        button.addEventListener("click", () => setTab((button.dataset.tab || "codex")));
    });
    dom.commandForm.addEventListener("submit", (event) => {
        event.preventDefault();
        const command = dom.codexCommand.value.trim();
        if (!command) {
            return;
        }
        state.commandHistory.push(command);
        dom.runPrompt.value = command;
        dom.codexCommand.value = "";
        renderCodex(selectedJob());
        renderInspector();
        setHealth("command staged");
    });
}
wireEvents();
renderAll();
void refresh();
