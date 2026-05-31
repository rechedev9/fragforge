package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZVBinaryRepoSkillWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := repoSkillWorkflowRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no repo skill workflow run commands found")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seen := make(map[string]bool)
	wantWorkflows := map[string]bool{
		"demo-parse":    true,
		"utility-audit": true,
		"record":        true,
		"shorts-render": true,
		"gallery-open":  true,
	}
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, command := range commands {
		workflowName := command[2]
		seen[workflowName] = true
		workflow, ok := findWorkflow(workflowName)
		if !ok {
			t.Fatalf("workflow %q from repo skill is not cataloged", workflowName)
		}
		if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
			wantOpenPathCalls++
		} else {
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, command...)
	}

	for workflowName := range wantWorkflows {
		if !seen[workflowName] {
			t.Fatalf("repo skills do not exercise workflow %q; saw %#v", workflowName, seen)
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryRepoSkillRequiredWorkflowRunsBySkillEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := repoSkillWorkflowRunCommandsBySkill(t, root)
	if len(commands) == 0 {
		t.Fatalf("no repo skill workflow run commands found")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seenBySkill := make(map[string]map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, entry := range commands {
		workflowName := entry.command[2]
		workflow, ok := findWorkflow(workflowName)
		if !ok {
			t.Fatalf("workflow %q from repo skill %s is not cataloged", workflowName, entry.skillName)
		}
		if seenBySkill[entry.skillName] == nil {
			seenBySkill[entry.skillName] = make(map[string]bool)
		}
		seenBySkill[entry.skillName][workflowName] = true
		if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
			wantOpenPathCalls++
		} else {
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, entry.command...)
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		if _, ok := seenBySkill[skillName]; !ok {
			t.Fatalf("repo skill %q did not execute any workflow runs; saw %#v", skillName, seenBySkill)
		}
		for _, workflowName := range requiredWorkflows {
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("repo skill %q did not execute required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryRepoSkillWorkflowRunsMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := repoSkillWorkflowRunCommandsBySkill(t, root)
	if len(commands) == 0 {
		t.Fatalf("no repo skill workflow run commands found")
	}

	seenBySkill := make(map[string]map[string]bool)
	for i, entry := range commands {
		if len(entry.command) < 3 {
			t.Fatalf("repo skill %s workflow run command = %#v, want workflows run <name>", entry.skillName, entry.command)
		}
		workflow, ok := findWorkflow(entry.command[2])
		if !ok {
			t.Fatalf("workflow %q from repo skill %s is not cataloged", entry.command[2], entry.skillName)
		}
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		if seenBySkill[entry.skillName] == nil {
			seenBySkill[entry.skillName] = make(map[string]bool)
		}
		seenBySkill[entry.skillName][workflow.Name] = true

		t.Run(fmt.Sprintf("%02d/%s/%s", i, entry.skillName, workflow.Name), func(t *testing.T) {
			directArgs := directArgsForWorkflowRunDocCommand(t, workflow, entry.command)
			runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-run.jsonl", i, entry.skillName, workflow.Name))
			directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-direct.jsonl", i, entry.skillName, workflow.Name))
			runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-run-open.txt", i, entry.skillName, workflow.Name))
			directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-direct-open.txt", i, entry.skillName, workflow.Name))

			runOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, entry.command...)
			directOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, directArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("repo skill workflow run output = %q, want direct output %q", got, want)
			}
			if workflow.Name == "gallery-open" {
				if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
					t.Fatalf("repo skill workflow run open path log = %q, want direct log %q", got, want)
				}
				return
			}

			runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
			directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("repo skill workflow run executable = %q, want direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("repo skill workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		for _, workflowName := range requiredWorkflows {
			workflow, ok := findWorkflow(workflowName)
			if !ok || !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("repo skill %q did not compare required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}
}

func TestZVBinarySkillsShowWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	var wantSubcommandCalls, wantOpenPathCalls int
	for _, skill := range currentRepoSkills(t, root) {
		showText := runZVBinary(t, exe, root, "skills", "show", skill.Name)
		showJSON := runZVBinary(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		if detail.Name != skill.Name {
			t.Fatalf("skills show json name = %q, want %q", detail.Name, skill.Name)
		}

		textCommands := skillWorkflowRunCommandsFromBody(t, showText)
		jsonCommands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if got, want := commandKeys(textCommands), commandKeys(jsonCommands); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("skills show text commands for %s = %#v, want json commands %#v", skill.Name, got, want)
		}
		if len(textCommands) == 0 {
			t.Fatalf("skills show %s did not expose workflow run commands", skill.Name)
		}

		seen := make(map[string]bool)
		for _, command := range textCommands {
			workflowName := command[2]
			seen[workflowName] = true
			workflow, ok := findWorkflow(workflowName)
			if !ok {
				t.Fatalf("workflow %q from skills show %s is not cataloged", workflowName, skill.Name)
			}
			if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
				wantOpenPathCalls++
			} else {
				wantSubcommandCalls++
			}
			runZVBinaryWithEnv(t, exe, root, env, command...)
		}

		for _, workflowName := range skillWorkflowRequirements(skill.Name) {
			if !seen[workflowName] {
				t.Fatalf("skills show %s did not expose required workflow %q; saw %#v", skill.Name, workflowName, seen)
			}
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinarySkillsShowJSONWorkflowRunsMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	seenBySkill := make(map[string]map[string]bool)
	for _, skill := range currentRepoSkills(t, root) {
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		commands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if len(commands) == 0 {
			t.Fatalf("skills show json for %s exposed no workflow run commands", skill.Name)
		}
		seenBySkill[skill.Name] = make(map[string]bool)

		for i, command := range commands {
			if len(command) < 3 {
				t.Fatalf("skills show json for %s exposed workflow command %#v, want workflows run <name>", skill.Name, command)
			}
			workflow, ok := findWorkflow(command[2])
			if !ok {
				t.Fatalf("workflow %q from skills show json for %s is not cataloged", command[2], skill.Name)
			}
			if !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			seenBySkill[skill.Name][workflow.Name] = true

			t.Run(fmt.Sprintf("%s/%02d/%s", skill.Name, i, workflow.Name), func(t *testing.T) {
				directArgs := directArgsForWorkflowRunDocCommand(t, workflow, command)
				runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-run.jsonl", skill.Name, i, workflow.Name))
				directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-direct.jsonl", skill.Name, i, workflow.Name))
				runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-run-open.txt", skill.Name, i, workflow.Name))
				directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-direct-open.txt", skill.Name, i, workflow.Name))

				runOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
				}, command...)
				directOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
				}, directArgs...)

				if got, want := runOut, directOut; got != want {
					t.Fatalf("skills show json workflow run output = %q, want direct output %q", got, want)
				}
				if workflow.Name == "gallery-open" {
					if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
						t.Fatalf("skills show json workflow run open path log = %q, want direct log %q", got, want)
					}
					return
				}

				runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
				directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
				if got, want := len(runCalls), 1; got != want {
					t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
				}
				if got, want := len(directCalls), 1; got != want {
					t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
				}
				if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
					t.Fatalf("skills show json workflow run executable = %q, want direct executable %q", got, want)
				}
				if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
					t.Fatalf("skills show json workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
				}
			})
		}
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		for _, workflowName := range requiredWorkflows {
			workflow, ok := findWorkflow(workflowName)
			if !ok || !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("skills show json for %q did not compare required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}
}

func TestZVBinarySkillsListJSONDiscoveryWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	listJSON, listStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if listStderr != "" {
		t.Fatalf("skills list json wrote stderr %q", listStderr)
	}
	var skills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &skills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	if len(skills) == 0 {
		t.Fatalf("skills list json returned no skills")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seenSkills := make(map[string]bool)
	seenBySkill := make(map[string]map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, skill := range skills {
		seenSkills[skill.Name] = true
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		commands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if len(commands) == 0 {
			t.Fatalf("skills show json for %s exposed no workflow run commands", skill.Name)
		}
		seenBySkill[skill.Name] = make(map[string]bool)
		for _, command := range commands {
			workflowName := command[2]
			workflow, ok := findWorkflow(workflowName)
			if !ok {
				t.Fatalf("workflow %q from skills list discovery for %s is not cataloged", workflowName, skill.Name)
			}
			seenBySkill[skill.Name][workflowName] = true
			if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
				wantOpenPathCalls++
			} else {
				wantSubcommandCalls++
			}
			runZVBinaryWithEnv(t, exe, root, env, command...)
		}
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		if !seenSkills[skillName] {
			t.Fatalf("skills list json did not expose required repo skill %q; saw %#v", skillName, seenSkills)
		}
		for _, workflowName := range requiredWorkflows {
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("skills list json discovery for %s did not expose required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinarySkillsDiscoveryJSONRequiredWorkflowRunsMatchContractEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	listJSON, listStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if listStderr != "" {
		t.Fatalf("skills list json wrote stderr %q", listStderr)
	}
	var skills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &skills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	seenSkills := make(map[string]bool)
	for _, skill := range skills {
		required := skillWorkflowRequirements(skill.Name)
		if len(required) == 0 {
			continue
		}
		seenSkills[skill.Name] = true
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		var got []string
		for _, command := range skillWorkflowRunCommandsFromBody(t, detail.Body) {
			got = append(got, command[2])
		}
		if strings.Join(got, "\x00") != strings.Join(required, "\x00") {
			t.Fatalf("skills show json workflow runs for %s = %#v, want %#v", skill.Name, got, required)
		}
	}
	for skillName := range skillWorkflowRequirementMap() {
		if !seenSkills[skillName] {
			t.Fatalf("skills list json did not expose required repo skill %q; saw %#v", skillName, seenSkills)
		}
	}
}

func TestZVBinaryUtilityShortsSkillRecordWorkflowDocumentsCaptureToolsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", "zackvideo-cs2-utility-shorts", "--format", "json")
	if showStderr != "" {
		t.Fatalf("skills show json stderr = %q, want empty", showStderr)
	}
	var detail skillDetail
	if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
		t.Fatalf("unmarshal skills show json: %v\n%s", err, showJSON)
	}

	var recordCommand []string
	for _, command := range skillWorkflowRunCommandsFromBody(t, detail.Body) {
		if len(command) >= 3 && command[0] == "workflows" && command[1] == "run" && command[2] == "record" {
			recordCommand = command
			break
		}
	}
	if len(recordCommand) == 0 {
		t.Fatalf("zackvideo-cs2-utility-shorts did not document workflows run record")
	}
	for _, want := range []string{"--killplan", "--demo", "--out", "--hlae", "--cs2"} {
		if !containsString(recordCommand, want) {
			t.Fatalf("record workflow command = %#v, want flag %s", recordCommand, want)
		}
	}

	subcommandLogPath := filepath.Join(tempDir, "utility-shorts-record.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
	}
	stdout, stderr := runZVBinarySplitWithEnv(t, exe, root, env, recordCommand...)
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	calls := readFakeSubcommandCalls(t, subcommandLogPath)
	if got, want := len(calls), 1; got != want {
		t.Fatalf("subcommand calls = %d, want %d: %#v", got, want, calls)
	}
	if got, want := calls[0].Executable, executableName("zv-recorder"); got != want {
		t.Fatalf("executable = %q, want %q", got, want)
	}
	for _, want := range []string{"--killplan", "--demo", "--out", "--hlae", "--cs2"} {
		if !containsString(calls[0].Args, want) {
			t.Fatalf("delegated record args = %#v, want flag %s", calls[0].Args, want)
		}
	}
}

func TestZVBinaryCurrentDocsAndSkillsRecordExamplesDocumentDryRunOrCaptureToolsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	type publishedRecordCommand struct {
		source  string
		command []string
	}

	var commands []publishedRecordCommand
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			if _, ok := recordCommandArgsForPublishedExample(command); !ok {
				continue
			}
			commands = append(commands, publishedRecordCommand{
				source:  doc.path,
				command: command,
			})
		}
	}
	for _, skill := range currentRepoSkillBodies(t, root) {
		for _, line := range skillCommandLines(skill.body) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			if _, ok := recordCommandArgsForPublishedExample(command); !ok {
				continue
			}
			commands = append(commands, publishedRecordCommand{
				source:  skill.path,
				command: command,
			})
		}
	}
	if len(commands) == 0 {
		t.Fatalf("no published record examples found in current docs or skills")
	}

	for _, command := range commands {
		args, ok := recordCommandArgsForPublishedExample(command.command)
		if !ok {
			continue
		}
		if recordCommandHasDryRunOrCaptureTools(args) {
			continue
		}
		t.Fatalf("%s: record example must include --dry-run or both --hlae and --cs2: %#v", command.source, command.command)
	}
}

func TestZVBinarySkillsCheckRejectsRecordExamplesWithoutDryRunOrCaptureToolsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := `missing required flags --hlae, --cs2 for "record" unless --dry-run is set`
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsRequiredWorkflowRunsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-cs2-utility-shorts", strings.Join([]string{
		"---",
		"name: zackvideo-cs2-utility-shorts",
		`description: "Create CS2 utility Shorts from a demo with ZackVideo."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording --dry-run`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "required workflow runs must appear in order: demo-parse, utility-audit, record, shorts-render, gallery-open"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsRequiredWorkflowRunDocumentedOnlyAsHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-lineup-audit", strings.Join([]string{
		"---",
		"name: zackvideo-lineup-audit",
		`description: "Review and correct ZackVideo CS2 utility destination labels."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run utility-audit -- --help`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "missing required workflow run utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsCatalogWorkflowRunsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "workflow runs must follow catalog order; demo-parse appears after shorts-render"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsUnexpectedRequiredSkillWorkflowRunsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-lineup-audit", strings.Join([]string{
		"---",
		"name: zackvideo-lineup-audit",
		`description: "Review and correct ZackVideo CS2 utility destination labels."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "unexpected workflow run gallery-open; expected only: utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsZackVideoSkillWithoutWorkflowRequirementsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-cs2-utility-shorts", strings.Join([]string{
		"---",
		"name: zackvideo-cs2-utility-shorts",
		`description: "Create CS2 utility Shorts from a demo with ZackVideo."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording --dry-run`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))
	writeSkillBody(t, tempDir, "zackvideo-lineup-audit", strings.Join([]string{
		"---",
		"name: zackvideo-lineup-audit",
		`description: "Review and correct ZackVideo CS2 utility destination labels."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		"```",
		"",
	}, "\n"))
	writeSkillBody(t, tempDir, "zackvideo-youtube-shorts-publish", strings.Join([]string{
		"---",
		"name: zackvideo-youtube-shorts-publish",
		`description: "Prepare or upload ZackVideo YouTube Shorts publish packs."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))
	writeSkillBody(t, tempDir, "zackvideo-new-skill", strings.Join([]string{
		"---",
		"name: zackvideo-new-skill",
		`description: "New ZackVideo workflow skill."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "skill:zackvideo-new-skill: missing workflow requirements for repo skill"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsMissingRequiredRepoSkillEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-cs2-utility-shorts", strings.Join([]string{
		"---",
		"name: zackvideo-cs2-utility-shorts",
		`description: "Create CS2 utility Shorts from a demo with ZackVideo."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording --dry-run`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"skill:zackvideo-lineup-audit: workflow requirements reference missing repo skill",
		"skill:zackvideo-shorts-production: workflow requirements reference missing repo skill",
		"skill:zackvideo-youtube-shorts-publish: workflow requirements reference missing repo skill",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinarySkillsCheckRejectsDuplicateWorkflowRunsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run demo-parse -- --demo other.dem --steamid 76561198000000000 --out other-plan.json`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	if !hasIssueContaining(result.Issues, "duplicate workflow run demo-parse") {
		t.Fatalf("issues = %#v, want duplicate workflow run issue", result.Issues)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocsRecordExamplesWithoutDryRunOrCaptureToolsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "docs", "toolchain.md"), strings.Join([]string{
		"# Toolchain",
		"",
		"`zv check` validates the unified CLI contract.",
		"",
		"```powershell",
		`.\bin\zv.exe record --killplan plan.json --demo demo.dem --out recording`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := `docs/toolchain.md: missing required flags --hlae, --cs2 for "record" unless --dry-run is set`
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocRunDocumentedOnlyAsHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	readme := readFileString(t, readmePath)
	old := "./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json"
	if !strings.Contains(readme, old) {
		t.Fatalf("README fixture does not contain expected workflow run command")
	}
	readme = strings.ReplaceAll(readme, old, "./bin/zv workflows run demo-parse -- --help")
	writeFile(t, readmePath, readme)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: missing executable workflow run demo-parse"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDirectCommandDocumentedOnlyAsHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	readme := readFileString(t, readmePath)
	old := "./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json"
	if !strings.Contains(readme, old) {
		t.Fatalf("README fixture does not contain expected direct workflow command")
	}
	readme = strings.ReplaceAll(readme, old, "./bin/zv demo parse --help")
	writeFile(t, readmePath, readme)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: missing executable workflow command demo-parse"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDirectCommandMissingRequiredFlagEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	readme := readFileString(t, readmePath)
	old := "./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json"
	if !strings.Contains(readme, old) {
		t.Fatalf("README fixture does not contain expected direct workflow command")
	}
	readme = strings.ReplaceAll(readme, old, "./bin/zv demo parse --demo testdata/foo.dem --out plan.json")
	writeFile(t, readmePath, readme)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		`README.md: missing required flag --steamid for "demo parse"`,
		"README.md: missing executable workflow command demo-parse",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocRunCommandsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	b, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README fixture: %v", err)
	}
	old := strings.Join([]string{
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
	}, "\n")
	replacement := strings.Join([]string{
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
	}, "\n")
	body := string(b)
	if !strings.Contains(body, old) {
		t.Fatalf("README fixture does not contain expected workflow run order")
	}
	writeFile(t, readmePath, strings.Replace(body, old, replacement, 1))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: workflow run commands must appear in catalog order: demo-parse, demo-players, utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocShowCommandsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	b, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README fixture: %v", err)
	}
	old := strings.Join([]string{
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
	}, "\n")
	replacement := strings.Join([]string{
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
	}, "\n")
	body := string(b)
	if !strings.Contains(body, old) {
		t.Fatalf("README fixture does not contain expected workflow show order")
	}
	writeFile(t, readmePath, strings.Replace(body, old, replacement, 1))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: workflow show commands must appear in catalog order: demo-parse, demo-players, utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateWorkflowDocShowCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate workflow show demo-parse --format text",
		"README.md: duplicate workflow show demo-parse --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateWorkflowDocListAndCheckCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format=json",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate workflows list --format text",
		"README.md: duplicate workflows list --format json",
		"README.md: duplicate workflows check --format text",
		"README.md: duplicate workflows check --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateProjectCheckDocCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv check",
		"./bin/zv check --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate check --format text",
		"README.md: duplicate check --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateSkillDocShowCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv skills show alpha",
		"./bin/zv skills show alpha --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate skill show alpha --format text",
		"README.md: duplicate skill show alpha --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateSkillDocListAndCheckCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv skills list",
		"./bin/zv skills list",
		"./bin/zv skills list --format=json",
		"./bin/zv skills check",
		"./bin/zv skills check --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate skills list --format text",
		"README.md: duplicate skills list --format json",
		"README.md: duplicate skills check --format text",
		"README.md: duplicate skills check --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsSkillDocShowCommandsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	for _, name := range []string{"alpha", "beta"} {
		writeSkillBody(t, tempDir, name, strings.Join([]string{
			"---",
			"name: " + name,
			`description: "Test workflow"`,
			"---",
			"",
			"```powershell",
			`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
			"```",
			"",
		}, "\n"))
	}
	writeWorkflowDocs(t, tempDir)

	readmePath := filepath.Join(tempDir, "README.md")
	replaceSkillShowFixture(t, readmePath, strings.Join([]string{
		"./bin/zv skills show beta",
		"./bin/zv skills show alpha",
	}, "\n"), strings.Join([]string{
		"./bin/zv skills show beta --format json",
		"./bin/zv skills show alpha --format json",
	}, "\n"))
	codexReadmePath := filepath.Join(tempDir, ".codex", "README.md")
	replaceSkillShowFixture(t, codexReadmePath, strings.Join([]string{
		"./bin/zv skills show alpha",
		"./bin/zv skills show beta",
	}, "\n"), strings.Join([]string{
		"./bin/zv skills show alpha --format json",
		"./bin/zv skills show beta --format json",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: skill show commands must appear in skill order: alpha, beta"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateWorkflowDocRunCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv workflows run demo-parse -- --demo testdata/other.dem --steamid 76561198000000000 --out other-plan.json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	if !hasIssueContaining(result.Issues, "README.md: duplicate workflow run demo-parse") {
		t.Fatalf("issues = %#v, want duplicate workflow run issue", result.Issues)
	}
}

func TestZVBinarySkillsListJSONDiscoveryWorkflowRunsMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	listJSON, listStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if listStderr != "" {
		t.Fatalf("skills list json wrote stderr %q", listStderr)
	}
	var skills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &skills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	if len(skills) == 0 {
		t.Fatalf("skills list json returned no skills")
	}

	seenSkills := make(map[string]bool)
	seenBySkill := make(map[string]map[string]bool)
	for _, skill := range skills {
		seenSkills[skill.Name] = true
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		commands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if len(commands) == 0 {
			t.Fatalf("skills list json discovery for %s exposed no workflow run commands", skill.Name)
		}
		seenBySkill[skill.Name] = make(map[string]bool)

		for i, command := range commands {
			if len(command) < 3 {
				t.Fatalf("skills list json discovery for %s exposed workflow command %#v, want workflows run <name>", skill.Name, command)
			}
			workflow, ok := findWorkflow(command[2])
			if !ok {
				t.Fatalf("workflow %q from skills list json discovery for %s is not cataloged", command[2], skill.Name)
			}
			if !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			seenBySkill[skill.Name][workflow.Name] = true

			t.Run(fmt.Sprintf("%s/%02d/%s", skill.Name, i, workflow.Name), func(t *testing.T) {
				directArgs := directArgsForWorkflowRunDocCommand(t, workflow, command)
				runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-run.jsonl", skill.Name, i, workflow.Name))
				directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-direct.jsonl", skill.Name, i, workflow.Name))
				runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-run-open.txt", skill.Name, i, workflow.Name))
				directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-direct-open.txt", skill.Name, i, workflow.Name))

				runOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
				}, command...)
				directOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
				}, directArgs...)

				if got, want := runOut, directOut; got != want {
					t.Fatalf("skills list json workflow run output = %q, want direct output %q", got, want)
				}
				if workflow.Name == "gallery-open" {
					if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
						t.Fatalf("skills list json workflow run open path log = %q, want direct log %q", got, want)
					}
					return
				}

				runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
				directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
				if got, want := len(runCalls), 1; got != want {
					t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
				}
				if got, want := len(directCalls), 1; got != want {
					t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
				}
				if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
					t.Fatalf("skills list json workflow run executable = %q, want direct executable %q", got, want)
				}
				if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
					t.Fatalf("skills list json workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
				}
			})
		}
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		if !seenSkills[skillName] {
			t.Fatalf("skills list json did not expose required repo skill %q; saw %#v", skillName, seenSkills)
		}
		for _, workflowName := range requiredWorkflows {
			workflow, ok := findWorkflow(workflowName)
			if !ok || !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("skills list json discovery for %q did not compare required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}
}

func TestZVBinaryCurrentWorkflowDocExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow run commands found in current workflow docs")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seen := make(map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, command := range commands {
		workflowName := command[2]
		seen[workflowName] = true
		workflow, ok := findWorkflow(workflowName)
		if !ok {
			t.Fatalf("workflow %q from docs is not cataloged", workflowName)
		}
		switch {
		case len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open":
			wantOpenPathCalls++
		case workflow.RunArgs[0] == "skills" || workflow.RunArgs[0] == "workflows" || workflow.RunArgs[0] == "check":
		default:
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, command...)
	}

	for _, workflow := range workflowCatalog() {
		if !seen[workflow.Name] {
			t.Fatalf("workflow docs do not exercise workflow %q; saw %#v", workflow.Name, seen)
		}
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryCurrentDirectDocExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocDirectCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no direct zv commands found in current workflow docs")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	documented := make(map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, command := range commands {
		if got := documentedWorkflowCommand("zv " + strings.Join(command, " ")); got != "" {
			documented[got] = true
		}
		switch command[0] {
		case "gallery":
			wantOpenPathCalls++
		case "demo", "utility", "record", "compose", "shorts", "music", "analysis", "serve", "pipeline":
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, command...)
	}

	for _, workflow := range workflowCatalog() {
		want := documentedWorkflowCommand(workflow.Command)
		if want != "" && !documented[want] {
			t.Fatalf("workflow docs do not execute direct command %q; saw %#v", want, documented)
		}
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryCurrentRequiredWorkflowDocsCoverEveryExecutableWorkflowEndToEnd(t *testing.T) {
	root := repoRoot(t)
	var docsChecked int
	for _, doc := range workflowDocs() {
		if !doc.RequiredWorkflows {
			continue
		}
		docsChecked++
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}

		directSeen := make(map[string]bool)
		runSeen := make(map[string]bool)
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			if isExecutableWorkflowRunCommand(command) {
				runSeen[command[2]] = true
			}
			for _, workflow := range workflowCatalog() {
				if isExecutableDirectWorkflowCommand(command, workflow) {
					directSeen[workflow.Name] = true
					break
				}
			}
		}

		for _, workflow := range workflowCatalog() {
			if strings.TrimSpace(workflow.Name) == "" {
				continue
			}
			if documentedWorkflowCommand(workflow.Command) != "" && !directSeen[workflow.Name] {
				t.Fatalf("%s does not document executable direct command for workflow %q; saw %#v", doc.Path, workflow.Name, directSeen)
			}
			if !runSeen[workflow.Name] {
				t.Fatalf("%s does not document executable workflow run for workflow %q; saw %#v", doc.Path, workflow.Name, runSeen)
			}
		}
	}
	if docsChecked == 0 {
		t.Fatalf("no required workflow docs checked")
	}
}

func TestZVBinaryCurrentRequiredSkillDocsCoverEverySkillCommandEndToEnd(t *testing.T) {
	root := repoRoot(t)
	skills := currentRepoSkills(t, root)
	var docsChecked int
	for _, doc := range workflowDocs() {
		if !doc.RequiredSkills {
			continue
		}
		docsChecked++
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}

		listCheckSeen := make(map[string]bool)
		showSeen := make(map[string]map[string]bool)
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "skills" {
				continue
			}
			switch command[1] {
			case "list", "check":
				format, rest, err := parseFormatArgs(command[2:])
				if err != nil || len(rest) != 0 {
					continue
				}
				listCheckSeen[command[1]+":"+format] = true
			case "show":
				if len(command) < 3 {
					continue
				}
				format, rest, err := parseFormatArgs(command[3:])
				if err != nil || len(rest) != 0 {
					continue
				}
				if showSeen[command[2]] == nil {
					showSeen[command[2]] = make(map[string]bool)
				}
				showSeen[command[2]][format] = true
			}
		}

		for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
			if !listCheckSeen[want] {
				t.Fatalf("%s does not document skills %s; saw %#v", doc.Path, strings.ReplaceAll(want, ":", " "), listCheckSeen)
			}
		}
		for _, skill := range skills {
			for _, format := range []string{"text", "json"} {
				if !showSeen[skill.Name][format] {
					t.Fatalf("%s does not document skills show %s with %s format; saw %#v", doc.Path, skill.Name, format, showSeen[skill.Name])
				}
			}
		}
	}
	if docsChecked == 0 {
		t.Fatalf("no required skill docs checked")
	}
}

func TestZVBinaryCurrentRequiredWorkflowDocsCoverEveryDiscoveryCommandEndToEnd(t *testing.T) {
	root := repoRoot(t)
	var docsChecked int
	for _, doc := range workflowDocs() {
		if !doc.RequiredWorkflows {
			continue
		}
		docsChecked++
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}

		listCheckSeen := make(map[string]bool)
		showSeen := make(map[string]map[string]bool)
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "workflows" {
				continue
			}
			switch command[1] {
			case "list", "check":
				format, rest, err := parseFormatArgs(command[2:])
				if err != nil || len(rest) != 0 {
					continue
				}
				listCheckSeen[command[1]+":"+format] = true
			case "show":
				if len(command) < 3 {
					continue
				}
				format, rest, err := parseFormatArgs(command[3:])
				if err != nil || len(rest) != 0 {
					continue
				}
				if showSeen[command[2]] == nil {
					showSeen[command[2]] = make(map[string]bool)
				}
				showSeen[command[2]][format] = true
			}
		}

		for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
			if !listCheckSeen[want] {
				t.Fatalf("%s does not document workflows %s; saw %#v", doc.Path, strings.ReplaceAll(want, ":", " "), listCheckSeen)
			}
		}
		for _, workflow := range workflowCatalog() {
			for _, format := range []string{"text", "json"} {
				if !showSeen[workflow.Name][format] {
					t.Fatalf("%s does not document workflows show %s with %s format; saw %#v", doc.Path, workflow.Name, format, showSeen[workflow.Name])
				}
			}
		}
	}
	if docsChecked == 0 {
		t.Fatalf("no required workflow docs checked")
	}
}

func TestZVBinaryCurrentDirectDocExamplesMatchWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocDirectCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no direct zv commands found in current workflow docs")
	}

	seen := make(map[string]bool)
	for i, directArgs := range commands {
		workflow, ok := workflowForDirectCommand(directArgs)
		if !ok || !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		seen[workflow.Name] = true

		t.Run(fmt.Sprintf("%02d/%s", i, workflow.Name), func(t *testing.T) {
			runArgs := workflowRunArgsForDirectCommand(t, workflow, directArgs)
			directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-direct.jsonl", i, workflow.Name))
			runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run.jsonl", i, workflow.Name))
			directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-direct-open.txt", i, workflow.Name))
			runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-open.txt", i, workflow.Name))

			directOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, directArgs...)
			runOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, runArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("workflow run output = %q, want documented direct output %q", got, want)
			}
			if workflow.Name == "gallery-open" {
				if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
					t.Fatalf("workflow run open path log = %q, want direct log %q", got, want)
				}
				return
			}

			directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
			runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("workflow run executable = %q, want documented direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("workflow run args = %#v, want documented direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}

	for _, workflow := range workflowCatalog() {
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		if !seen[workflow.Name] {
			t.Fatalf("current workflow docs do not compare direct command for workflow %q", workflow.Name)
		}
	}
}

func TestZVBinaryCurrentWorkflowRunDocExamplesMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow run commands found in current workflow docs")
	}

	seen := make(map[string]bool)
	for i, runArgs := range commands {
		if len(runArgs) < 3 {
			t.Fatalf("workflow run command = %#v, want workflows run <name>", runArgs)
		}
		workflow, ok := findWorkflow(runArgs[2])
		if !ok {
			t.Fatalf("workflow %q from docs is not cataloged", runArgs[2])
		}
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		seen[workflow.Name] = true

		t.Run(fmt.Sprintf("%02d/%s", i, workflow.Name), func(t *testing.T) {
			directArgs := directArgsForWorkflowRunDocCommand(t, workflow, runArgs)
			directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-direct.jsonl", i, workflow.Name))
			runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-run.jsonl", i, workflow.Name))
			directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-direct-open.txt", i, workflow.Name))
			runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-run-open.txt", i, workflow.Name))

			runOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, runArgs...)
			directOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, directArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("documented workflow run output = %q, want direct output %q", got, want)
			}
			if workflow.Name == "gallery-open" {
				if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
					t.Fatalf("documented workflow run open path log = %q, want direct log %q", got, want)
				}
				return
			}

			runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
			directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("documented workflow run executable = %q, want direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("documented workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}

	for _, workflow := range workflowCatalog() {
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		if !seen[workflow.Name] {
			t.Fatalf("current workflow docs do not compare workflow run command for workflow %q", workflow.Name)
		}
	}
}

func TestZVBinaryCurrentWorkflowDocShowExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentWorkflowDocShowCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow show commands found in current workflow docs")
	}

	seenText := make(map[string]bool)
	seenJSON := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 3 {
			t.Fatalf("workflow show command = %#v, want workflows show <name>", command)
		}
		workflow, ok := findWorkflow(command[2])
		if !ok {
			t.Fatalf("workflow %q from docs is not cataloged", command[2])
		}
		format, rest, err := parseFormatArgs(command[3:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("workflow show command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}

		switch format {
		case "text":
			seenText[workflow.Name] = true
			if got, want := stdout, workflowShowText(workflow); got != want {
				t.Fatalf("workflow show text for %s = %q, want %q", workflow.Name, got, want)
			}
		case "json":
			seenJSON[workflow.Name] = true
			if strings.Contains(stdout, `"run_args"`) {
				t.Fatalf("workflow show json for %s leaked run_args: %s", workflow.Name, stdout)
			}
			var got workflowInfo
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("unmarshal workflow show json for %s: %v\n%s", workflow.Name, err, stdout)
			}
			if got.Name != workflow.Name || got.Description != workflow.Description || got.Command != workflow.Command || got.RunCommand != workflow.RunCommand {
				t.Fatalf("workflow show json for %s = %#v, want %#v", workflow.Name, got, workflow)
			}
		default:
			t.Fatalf("workflow show command %#v used unsupported format %q", command, format)
		}
	}

	for _, workflow := range workflowCatalog() {
		if !seenText[workflow.Name] {
			t.Fatalf("current workflow docs do not execute text workflow show for %q", workflow.Name)
		}
		if !seenJSON[workflow.Name] {
			t.Fatalf("current workflow docs do not execute json workflow show for %q", workflow.Name)
		}
	}
}

func TestZVBinaryCurrentSkillDocShowExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentSkillDocShowCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no skill show commands found in current workflow docs")
	}

	skills := currentRepoSkills(t, root)
	skillByName := make(map[string]skillInfo, len(skills))
	for _, skill := range skills {
		skillByName[skill.Name] = skill
	}

	seenText := make(map[string]bool)
	seenJSON := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 3 {
			t.Fatalf("skill show command = %#v, want skills show <name>", command)
		}
		skill, ok := skillByName[command[2]]
		if !ok {
			t.Fatalf("skill %q from docs is not a repo skill", command[2])
		}
		format, rest, err := parseFormatArgs(command[3:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("skill show command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}

		wantBody := readFileString(t, skill.Path)
		switch format {
		case "text":
			seenText[skill.Name] = true
			if strings.TrimRight(stdout, "\n") != strings.TrimRight(wantBody, "\n") {
				t.Fatalf("skill show text for %s did not match %s", skill.Name, skill.Path)
			}
		case "json":
			seenJSON[skill.Name] = true
			if strings.Contains(stdout, `"path"`) || strings.Contains(stdout, skill.Path) {
				t.Fatalf("skill show json for %s leaked local path: %s", skill.Name, stdout)
			}
			var got skillDetail
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("unmarshal skill show json for %s: %v\n%s", skill.Name, err, stdout)
			}
			if got.Name != skill.Name || got.Description != skill.Description || got.Body != wantBody {
				t.Fatalf("skill show json for %s = %#v, want name=%q description=%q body from %s", skill.Name, got, skill.Name, skill.Description, skill.Path)
			}
		default:
			t.Fatalf("skill show command %#v used unsupported format %q", command, format)
		}
	}

	for _, skill := range skills {
		if !seenText[skill.Name] {
			t.Fatalf("current workflow docs do not execute text skill show for %q", skill.Name)
		}
		if !seenJSON[skill.Name] {
			t.Fatalf("current workflow docs do not execute json skill show for %q", skill.Name)
		}
	}
}

func TestZVBinaryCurrentSkillDocListAndCheckExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentSkillDocListAndCheckCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no skill list/check commands found in current workflow docs")
	}

	skills := currentRepoSkills(t, root)
	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 2 {
			t.Fatalf("skill command = %#v, want skills list/check", command)
		}
		format, rest, err := parseFormatArgs(command[2:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("skill command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[command[1]+":"+format] = true

		switch command[1] {
		case "list":
			switch format {
			case "text":
				if got, want := stdout, skillListText(skills); got != want {
					t.Fatalf("documented skills list stdout = %q, want %q", got, want)
				}
			case "json":
				if strings.Contains(stdout, `"path"`) {
					t.Fatalf("documented skills list json leaked local path: %s", stdout)
				}
				var got []skillInfo
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented skills list json: %v\n%s", err, stdout)
				}
				if gotNames, wantNames := skillNames(got), skillNames(skills); strings.Join(gotNames, "\x00") != strings.Join(wantNames, "\x00") {
					t.Fatalf("documented skills list json names = %#v, want %#v", gotNames, wantNames)
				}
			default:
				t.Fatalf("skill list command %#v used unsupported format %q", command, format)
			}
		case "check":
			switch format {
			case "text":
				want := fmt.Sprintf("OK: %d skills checked\n", len(skills))
				if got := stdout; got != want {
					t.Fatalf("documented skills check stdout = %q, want %q", got, want)
				}
			case "json":
				var got skillCheckResult
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented skills check json: %v\n%s", err, stdout)
				}
				if !got.OK || got.SkillsChecked != len(skills) || len(got.Issues) != 0 {
					t.Fatalf("documented skills check json = %#v, want ok with %d skills and no issues", got, len(skills))
				}
			default:
				t.Fatalf("skill check command %#v used unsupported format %q", command, format)
			}
		default:
			t.Fatalf("unexpected skill command %#v", command)
		}
	}

	for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
		if !seen[want] {
			t.Fatalf("current workflow docs do not execute skills %s", strings.ReplaceAll(want, ":", " "))
		}
	}
}

func TestZVBinaryCurrentWorkflowDocListAndCheckExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentWorkflowDocListAndCheckCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow list/check commands found in current workflow docs")
	}

	workflows := workflowCatalog()
	workflowByName := make(map[string]workflowInfo, len(workflows))
	for _, workflow := range workflows {
		workflowByName[workflow.Name] = workflow
	}
	wantSkills := currentRepoSkills(t, root)
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantCheckText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(workflows), len(workflowDocs()), len(wantWrappers))

	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 2 {
			t.Fatalf("workflow command = %#v, want workflows list/check", command)
		}
		format, rest, err := parseFormatArgs(command[2:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("workflow command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[command[1]+":"+format] = true

		switch command[1] {
		case "list":
			switch format {
			case "text":
				if got, want := stdout, workflowListText(workflows); got != want {
					t.Fatalf("documented workflows list stdout = %q, want %q", got, want)
				}
			case "json":
				if strings.Contains(stdout, `"run_args"`) {
					t.Fatalf("documented workflows list json leaked run_args: %s", stdout)
				}
				var got []workflowInfo
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented workflows list json: %v\n%s", err, stdout)
				}
				if gotNames, wantNames := workflowNames(got), workflowNames(workflows); strings.Join(gotNames, "\x00") != strings.Join(wantNames, "\x00") {
					t.Fatalf("documented workflows list json names = %#v, want %#v", gotNames, wantNames)
				}
				for _, gotWorkflow := range got {
					wantWorkflow, ok := workflowByName[gotWorkflow.Name]
					if !ok {
						t.Fatalf("documented workflows list json returned unknown workflow %q", gotWorkflow.Name)
					}
					assertWorkflowDiscoveryMatches(t, "documented workflows list json", gotWorkflow, wantWorkflow)
				}
			default:
				t.Fatalf("workflow list command %#v used unsupported format %q", command, format)
			}
		case "check":
			switch format {
			case "text":
				if got := stdout; got != wantCheckText {
					t.Fatalf("documented workflows check stdout = %q, want %q", got, wantCheckText)
				}
			case "json":
				got := decodeWorkflowCheckResult(t, stdout)
				if !got.OK ||
					got.SkillsChecked != len(wantSkills) ||
					got.WorkflowsChecked != len(workflows) ||
					got.WorkflowDocsChecked != len(workflowDocs()) ||
					got.AgentPromptWrappersChecked != len(wantWrappers) ||
					len(got.Issues) != 0 {
					t.Fatalf("documented workflows check json = %#v, want ok with current repo counts", got)
				}
			default:
				t.Fatalf("workflow check command %#v used unsupported format %q", command, format)
			}
		default:
			t.Fatalf("unexpected workflow command %#v", command)
		}
	}

	for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
		if !seen[want] {
			t.Fatalf("current workflow docs do not execute workflows %s", strings.ReplaceAll(want, ":", " "))
		}
	}
}

func TestZVBinaryCurrentProjectCheckDocExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentProjectCheckDocCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no project check commands found in current workflow docs")
	}

	wantSkills := currentRepoSkills(t, root)
	wantWorkflows := workflowCatalog()
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(wantWorkflows), len(workflowDocs()), len(wantWrappers))

	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) == 0 || command[0] != "check" {
			t.Fatalf("project check command = %#v, want check", command)
		}
		format, rest, err := parseFormatArgs(command[1:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("project check command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[format] = true

		switch format {
		case "text":
			if got := stdout; got != wantText {
				t.Fatalf("documented project check stdout = %q, want %q", got, wantText)
			}
		case "json":
			got := decodeWorkflowCheckResult(t, stdout)
			if !got.OK ||
				got.SkillsChecked != len(wantSkills) ||
				got.WorkflowsChecked != len(wantWorkflows) ||
				got.WorkflowDocsChecked != len(workflowDocs()) ||
				got.AgentPromptWrappersChecked != len(wantWrappers) ||
				len(got.Issues) != 0 {
				t.Fatalf("documented project check json = %#v, want ok with current repo counts", got)
			}
		default:
			t.Fatalf("project check command %#v used unsupported format %q", command, format)
		}
	}

	for _, want := range []string{"text", "json"} {
		if !seen[want] {
			t.Fatalf("current workflow docs do not execute project check %s", want)
		}
	}
}

func TestZVBinaryCurrentInternalCheckWorkflowDocRunExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentInternalCheckWorkflowDocRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no internal check workflow run commands found in current workflow docs")
	}

	wantSkills := currentRepoSkills(t, root)
	wantWorkflows := workflowCatalog()
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantSkillText := fmt.Sprintf("OK: %d skills checked\n", len(wantSkills))
	wantWorkflowText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(wantWorkflows), len(workflowDocs()), len(wantWrappers))

	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 3 {
			t.Fatalf("internal check workflow run command = %#v, want workflows run <name>", command)
		}
		workflowName := command[2]
		format, err := workflowRunCheckFormat(command)
		if err != nil {
			t.Fatalf("internal check workflow run command %#v has invalid format args: %v", command, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[workflowName+":"+format] = true

		switch workflowName {
		case "skills-check":
			switch format {
			case "text":
				if got := stdout; got != wantSkillText {
					t.Fatalf("documented skills-check workflow stdout = %q, want %q", got, wantSkillText)
				}
			case "json":
				var got skillCheckResult
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented skills-check workflow json: %v\n%s", err, stdout)
				}
				if !got.OK || got.SkillsChecked != len(wantSkills) || len(got.Issues) != 0 {
					t.Fatalf("documented skills-check workflow json = %#v, want ok with %d skills and no issues", got, len(wantSkills))
				}
			default:
				t.Fatalf("skills-check workflow command %#v used unsupported format %q", command, format)
			}
		case "workflows-check", "project-check":
			switch format {
			case "text":
				if got := stdout; got != wantWorkflowText {
					t.Fatalf("documented %s workflow stdout = %q, want %q", workflowName, got, wantWorkflowText)
				}
			case "json":
				got := decodeWorkflowCheckResult(t, stdout)
				if !got.OK ||
					got.SkillsChecked != len(wantSkills) ||
					got.WorkflowsChecked != len(wantWorkflows) ||
					got.WorkflowDocsChecked != len(workflowDocs()) ||
					got.AgentPromptWrappersChecked != len(wantWrappers) ||
					len(got.Issues) != 0 {
					t.Fatalf("documented %s workflow json = %#v, want ok with current repo counts", workflowName, got)
				}
			default:
				t.Fatalf("%s workflow command %#v used unsupported format %q", workflowName, command, format)
			}
		default:
			t.Fatalf("unexpected internal check workflow command %#v", command)
		}
	}

	for _, workflowName := range []string{"skills-check", "workflows-check", "project-check"} {
		for _, format := range []string{"text", "json"} {
			key := workflowName + ":" + format
			if !seen[key] {
				t.Fatalf("current workflow docs do not execute %s in %s format", workflowName, format)
			}
		}
	}
}
