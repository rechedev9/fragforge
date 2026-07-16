package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func installFakeSubcommands(t *testing.T, binDir string, names ...string) {
	t.Helper()
	for _, name := range names {
		dst := filepath.Join(binDir, executableName(name))
		writeFakeSubcommandExecutable(t, dst)
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dst, 0o755); err != nil {
				t.Fatalf("chmod %s: %v", dst, err)
			}
		}
	}
}

func installFakeDelegatedSubcommands(t *testing.T, binDir string) {
	t.Helper()
	installFakeSubcommands(t, binDir, defaultLegacyCommandEntrypointNames()...)
}

var (
	fakeSubcommandMasterOnce sync.Once
	fakeSubcommandMasterDir  string
	fakeSubcommandMasterPath string
	fakeSubcommandMasterErr  error
)

func writeFakeSubcommandExecutable(t *testing.T, dst string) {
	t.Helper()
	fakeSubcommandMasterOnce.Do(func() {
		currentExe, err := os.Executable()
		if err != nil {
			fakeSubcommandMasterErr = fmt.Errorf("current executable: %w", err)
			return
		}
		fakeSubcommandMasterDir, fakeSubcommandMasterErr = os.MkdirTemp("", "zv-fake-subcommand-*")
		if fakeSubcommandMasterErr != nil {
			return
		}
		fakeSubcommandMasterPath = filepath.Join(fakeSubcommandMasterDir, executableName("zv-fake-subcommand"))
		contents, err := os.ReadFile(currentExe)
		if err != nil {
			fakeSubcommandMasterErr = fmt.Errorf("read current executable: %w", err)
			return
		}
		fakeSubcommandMasterErr = os.WriteFile(fakeSubcommandMasterPath, contents, 0o755)
	})
	if fakeSubcommandMasterErr != nil {
		t.Fatal(fakeSubcommandMasterErr)
	}
	if err := os.Link(fakeSubcommandMasterPath, dst); err != nil {
		copyFile(t, fakeSubcommandMasterPath, dst)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close %s: %v", dst, err)
	}
}

func readFakeSubcommandCalls(t *testing.T, path string) []fakeSubcommandCall {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open calls log: %v", err)
	}
	defer f.Close()
	var calls []fakeSubcommandCall
	dec := json.NewDecoder(f)
	for dec.More() {
		var call fakeSubcommandCall
		if err := dec.Decode(&call); err != nil {
			t.Fatalf("decode calls log: %v", err)
		}
		calls = append(calls, call)
	}
	return calls
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}

func currentRepoSkills(t *testing.T, root string) []skillInfo {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var skills []skillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			t.Fatalf("stat skill %s: %v", entry.Name(), err)
		}
		skill, err := parseSkill(path)
		if err != nil {
			t.Fatalf("parse skill %s: %v", path, err)
		}
		if skill.Name == "" {
			skill.Name = entry.Name()
		}
		skills = append(skills, skill)
	}
	if len(skills) == 0 {
		t.Fatalf("no repo skills found in %s", skillsDir)
	}
	return skills
}

func currentAgentPromptWrappers(t *testing.T, root string) []string {
	t.Helper()
	var wrappers []string
	for _, fixture := range currentCodexPromptWrappers(t, root) {
		wrappers = append(wrappers, fixture.wrapper)
	}
	for _, fixture := range currentClaudePromptWrappers(t, root) {
		wrappers = append(wrappers, fixture.wrapper)
	}
	return wrappers
}

func currentCodexPromptWrappers(t *testing.T, root string) []codexPromptWrapperFixture {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "scripts", "codex*.sh"))
	if err != nil {
		t.Fatalf("glob codex wrappers: %v", err)
	}
	var fixtures []codexPromptWrapperFixture
	for _, wrapper := range matches {
		if filepath.Base(wrapper) == "codex-run.sh" {
			continue
		}
		relWrapper := filepath.ToSlash(mustRel(root, wrapper))
		body := readFileString(t, wrapper)
		prompt, ok := codexWrapperPromptPath(body)
		if !ok {
			t.Fatalf("%s does not exec scripts/codex-run.sh with a prompt", relWrapper)
		}
		fixtures = append(fixtures, codexPromptWrapperFixture{
			wrapper: relWrapper,
			prompt:  prompt,
		})
	}
	if len(fixtures) == 0 {
		t.Fatalf("no codex prompt wrappers found")
	}
	return fixtures
}

func currentClaudePromptWrappers(t *testing.T, root string) []claudePromptWrapperFixture {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "scripts", "claude-zv-*.sh"))
	if err != nil {
		t.Fatalf("glob claude wrappers: %v", err)
	}
	var fixtures []claudePromptWrapperFixture
	for _, wrapper := range matches {
		relWrapper := filepath.ToSlash(mustRel(root, wrapper))
		body := readFileString(t, wrapper)
		command, ok := claudeWrapperCommandPath(body)
		if !ok {
			t.Fatalf("%s does not exec scripts/claude-run.sh with a command prompt", relWrapper)
		}
		fixtures = append(fixtures, claudePromptWrapperFixture{
			wrapper: relWrapper,
			command: command,
		})
	}
	if len(fixtures) == 0 {
		t.Fatalf("no claude prompt wrappers found")
	}
	return fixtures
}

func repoSkillWorkflowRunCommands(t *testing.T, root string) [][]string {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var commands [][]string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			commands = append(commands, command)
		}
	}
	return commands
}

type repoSkillWorkflowRunCommand struct {
	skillName string
	command   []string
}

func repoSkillWorkflowRunCommandsBySkill(t *testing.T, root string) []repoSkillWorkflowRunCommand {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var commands []repoSkillWorkflowRunCommand
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			commands = append(commands, repoSkillWorkflowRunCommand{
				skillName: entry.Name(),
				command:   command,
			})
		}
	}
	return commands
}

func skillWorkflowRunCommandsFromBody(t *testing.T, body string) [][]string {
	t.Helper()
	var commands [][]string
	for _, line := range skillCommandLines(body) {
		command, ok := skillCommand(line)
		if !ok || !isExecutableWorkflowRunCommand(command) {
			continue
		}
		commands = append(commands, command)
	}
	return commands
}

func currentWorkflowDocRunCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentWorkflowDocDirectCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || isWorkflowRunCommand(command) {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentWorkflowDocShowCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentSkillDocShowCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentSkillDocListAndCheckCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "skills" {
				continue
			}
			if command[1] != "list" && command[1] != "check" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentWorkflowDocListAndCheckCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "workflows" {
				continue
			}
			if command[1] != "list" && command[1] != "check" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentProjectCheckDocCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) == 0 || command[0] != "check" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentInternalCheckWorkflowDocRunCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, command := range currentWorkflowDocRunCommands(t, root) {
		if len(command) < 3 {
			continue
		}
		switch command[2] {
		case "skills-check", "workflows-check", "project-check":
		default:
			continue
		}
		key := strings.Join(command, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		commands = append(commands, command)
	}
	return commands
}

func workflowRunCheckFormat(command []string) (string, error) {
	if len(command) == 3 {
		return "text", nil
	}
	if len(command) < 4 || command[3] != "--" {
		return "", fmt.Errorf(`missing "--" separator before forwarded args`)
	}
	format, rest, err := parseFormatArgs(command[4:])
	if err != nil {
		return "", err
	}
	if len(rest) != 0 {
		return "", fmt.Errorf("unexpected forwarded args %q", strings.Join(rest, " "))
	}
	return format, nil
}

type workflowDocBody struct {
	path string
	body string
}

func currentWorkflowDocBodies(t *testing.T, root string) []workflowDocBody {
	t.Helper()
	var docs []workflowDocBody
	for _, doc := range workflowDocs() {
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}
		docs = append(docs, workflowDocBody{path: doc.Path, body: string(b)})
	}
	return docs
}

func currentRepoSkillBodies(t *testing.T, root string) []workflowDocBody {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var docs []workflowDocBody
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		relPath := filepath.ToSlash(filepath.Join(".codex", "skills", entry.Name(), "SKILL.md"))
		path := filepath.Join(root, filepath.FromSlash(relPath))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", relPath, err)
		}
		docs = append(docs, workflowDocBody{path: relPath, body: string(b)})
	}
	return docs
}

func recordCommandArgsForPublishedExample(command []string) ([]string, bool) {
	if len(command) > 0 && command[0] == "record" {
		return command[1:], true
	}
	if len(command) < 3 || command[0] != "workflows" || command[1] != "run" || command[2] != "record" {
		return nil, false
	}
	for i := 3; i < len(command); i++ {
		if command[i] == "--" {
			return command[i+1:], true
		}
	}
	return command[3:], true
}

func recordCommandHasDryRunOrCaptureTools(args []string) bool {
	return recordCommandDryRunIsTrue(args) ||
		(recordCommandFlagHasValue(args, "--hlae") && recordCommandFlagHasValue(args, "--cs2"))
}

func recordCommandDryRunIsTrue(args []string) bool {
	for i, arg := range args {
		if arg == "--dry-run" {
			if i+1 < len(args) {
				if value, err := strconv.ParseBool(args[i+1]); err == nil {
					return value
				}
			}
			return true
		}
		const prefix = "--dry-run="
		if strings.HasPrefix(arg, prefix) {
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, prefix))
			return err == nil && value
		}
	}
	return false
}

func recordCommandFlagHasValue(args []string, flag string) bool {
	for i, arg := range args {
		if arg == flag {
			return i+1 < len(args) && args[i+1] != "" && !strings.HasPrefix(args[i+1], "--")
		}
		if strings.HasPrefix(arg, flag+"=") {
			return strings.TrimPrefix(arg, flag+"=") != ""
		}
	}
	return false
}

func workflowRunCommandArgs(t *testing.T, workflow workflowInfo) []string {
	t.Helper()
	fields, ok := splitCommandFields(workflow.RunCommand)
	if !ok {
		t.Fatalf("parse workflow run command %q", workflow.RunCommand)
	}
	if len(fields) < 4 || fields[0] != "zv" || fields[1] != "workflows" || fields[2] != "run" {
		t.Fatalf("workflow run command = %#v, want zv workflows run <name>", fields)
	}
	return append([]string(nil), fields[1:]...)
}

func workflowRunSampleForwardedArgs(t *testing.T, workflow workflowInfo, galleryPath string) []string {
	t.Helper()
	switch workflow.Name {
	case "short":
		return []string{"--", "inferno.dem", "--prompt", "all kills 76561198000000000", "--out", "run/short", "--dry-run"}
	case "demo-parse":
		return []string{"--", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "run/plan.json"}
	case "demo-players":
		return []string{"--", "--demo", "inferno.dem"}
	case "utility-audit":
		return []string{"--", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"}
	case "record":
		return []string{"--", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"}
	case "compose-final":
		return []string{"--", "--recording-result", "run/recording/recording-result.json", "--out", "run/final.mp4", "--dry-run"}
	case "shorts-render":
		return []string{"--", "--recording-result", "run/recording/recording-result.json", "--out", "run/shorts"}
	case "music-analyze":
		return []string{"--", "--input", "data/music/track.mp4", "--out", "run/rhythm.json"}
	case "analysis-tactical-data":
		return []string{"--", "--demo", "inferno.dem", "--out", "run/tactical.json", "--start", "1000", "--end", "2000"}
	case "analysis-viewer":
		return []string{"--", "--json", "run/analysis.json"}
	case "gallery-open":
		return []string{"--", "--path", galleryPath}
	case "skills-check", "workflows-check", "project-check", "serve":
		return nil
	default:
		t.Fatalf("missing sample forwarded args for workflow %q", workflow.Name)
		return nil
	}
}

func workflowDirectSampleArgs(t *testing.T, workflow workflowInfo, galleryPath string) []string {
	t.Helper()
	args := append([]string(nil), workflow.RunArgs...)
	forwarded := workflowRunSampleForwardedArgs(t, workflow, galleryPath)
	if len(forwarded) == 0 {
		return args
	}
	if forwarded[0] != "--" {
		t.Fatalf("workflow %q sample forwarded args = %#v, want leading --", workflow.Name, forwarded)
	}
	return append(args, forwarded[1:]...)
}

func workflowRunSampleArgsWithoutSeparator(t *testing.T, workflow workflowInfo, galleryPath string) []string {
	t.Helper()
	forwarded := workflowRunSampleForwardedArgs(t, workflow, galleryPath)
	if len(forwarded) > 0 {
		if forwarded[0] != "--" {
			t.Fatalf("workflow %q sample forwarded args = %#v, want leading --", workflow.Name, forwarded)
		}
		return append([]string(nil), forwarded[1:]...)
	}
	switch workflow.Name {
	case "skills-check", "workflows-check", "project-check":
		return []string{"--format", "json"}
	case "serve":
		return []string{"--help"}
	default:
		t.Fatalf("missing separator sample args for workflow %q", workflow.Name)
		return nil
	}
}

func assertWorkflowDiscoveryMatches(t *testing.T, source string, got workflowInfo, want workflowInfo) {
	t.Helper()
	if got.Name != want.Name {
		t.Fatalf("%s name = %q, want %q", source, got.Name, want.Name)
	}
	if got.Description != want.Description {
		t.Fatalf("%s description for %s = %q, want %q", source, want.Name, got.Description, want.Description)
	}
	if got.Command != want.Command {
		t.Fatalf("%s command for %s = %q, want %q", source, want.Name, got.Command, want.Command)
	}
	if got.RunCommand != want.RunCommand {
		t.Fatalf("%s run_command for %s = %q, want %q", source, want.Name, got.RunCommand, want.RunCommand)
	}
	if got.ValidateCommand != want.ValidateCommand {
		t.Fatalf("%s validate_command for %s = %q, want %q", source, want.Name, got.ValidateCommand, want.ValidateCommand)
	}
	if got.RunArgs != nil {
		t.Fatalf("%s run args for %s = %#v, want omitted from json", source, want.Name, got.RunArgs)
	}
}

func assertJSONKeys(t *testing.T, source string, row map[string]json.RawMessage, want ...string) {
	t.Helper()
	wantSet := make(map[string]struct{}, len(want))
	for _, key := range want {
		wantSet[key] = struct{}{}
		if _, ok := row[key]; !ok {
			t.Fatalf("%s missing json key %q in %#v", source, key, row)
		}
	}
	for key := range row {
		if _, ok := wantSet[key]; !ok {
			t.Fatalf("%s has unexpected json key %q in %#v", source, key, row)
		}
	}
}

func assertIssueJSONKeys(t *testing.T, source string, raw json.RawMessage) {
	t.Helper()
	var issues []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &issues); err != nil {
		t.Fatalf("unmarshal %s: %v\n%s", source, err, raw)
	}
	if len(issues) == 0 {
		t.Fatalf("%s len = 0, want issues", source)
	}
	for i, issue := range issues {
		assertJSONKeys(t, fmt.Sprintf("%s[%d]", source, i), issue, "path", "message")
	}
}

func duplicateFlagValue(t *testing.T, args []string, flag string) []string {
	t.Helper()
	dup := append([]string(nil), args...)
	for i, arg := range dup {
		if arg != flag {
			continue
		}
		if i+1 >= len(dup) {
			t.Fatalf("flag %s has no value in %#v", flag, args)
		}
		insert := []string{flag, dup[i+1]}
		dup = append(dup[:i+2], append(insert, dup[i+2:]...)...)
		return dup
	}
	t.Fatalf("flag %s not found in %#v", flag, args)
	return nil
}

func equalsRequiredFlags(t *testing.T, args []string, required []string) []string {
	t.Helper()
	converted := append([]string(nil), args...)
	for _, flag := range required {
		var found bool
		for i := 0; i < len(converted); i++ {
			if converted[i] != flag {
				continue
			}
			if i+1 >= len(converted) {
				t.Fatalf("flag %s has no value in %#v", flag, args)
			}
			converted[i] = flag + "=" + converted[i+1]
			converted = append(converted[:i+1], converted[i+2:]...)
			found = true
			break
		}
		if !found {
			t.Fatalf("flag %s not found in %#v", flag, args)
		}
	}
	return converted
}

func emptyEqualsRequiredFlag(t *testing.T, args []string, flag string) []string {
	t.Helper()
	converted := append([]string(nil), args...)
	for i := 0; i < len(converted); i++ {
		if converted[i] != flag {
			continue
		}
		if i+1 >= len(converted) {
			t.Fatalf("flag %s has no value in %#v", flag, args)
		}
		converted[i] = flag + "="
		return append(converted[:i+1], converted[i+2:]...)
	}
	t.Fatalf("flag %s not found in %#v", flag, args)
	return nil
}

func emptySeparateRequiredFlag(t *testing.T, args []string, flag string) []string {
	t.Helper()
	converted := append([]string(nil), args...)
	for i := 0; i < len(converted); i++ {
		if converted[i] != flag {
			continue
		}
		if i+1 >= len(converted) {
			t.Fatalf("flag %s has no value in %#v", flag, args)
		}
		converted[i+1] = ""
		return converted
	}
	t.Fatalf("flag %s not found in %#v", flag, args)
	return nil
}

func skillListText(skills []skillInfo) string {
	var b strings.Builder
	for _, skill := range skills {
		if skill.Description == "" {
			fmt.Fprintln(&b, skill.Name)
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\n", skill.Name, skill.Description)
	}
	return b.String()
}

func skillNames(skills []skillInfo) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}

func workflowListText(workflows []workflowInfo) string {
	var b strings.Builder
	for _, workflow := range workflows {
		fmt.Fprintf(&b, "%s\t%s\n", workflow.Name, workflow.Description)
	}
	return b.String()
}

func workflowShowText(workflow workflowInfo) string {
	return fmt.Sprintf("%s\n%s\n\ncommand: %s\nrun_command: %s\nvalidate_command: %s\n", workflow.Name, workflow.Description, workflow.Command, workflow.RunCommand, workflow.ValidateCommand)
}

func decodeWorkflowCheckResult(t *testing.T, body string) workflowCheckResult {
	t.Helper()
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("unmarshal workflow check json: %v\n%s", err, body)
	}
	return result
}

func commandKeys(commands [][]string) []string {
	keys := make([]string, 0, len(commands))
	for _, command := range commands {
		keys = append(keys, strings.Join(command, "\x00"))
	}
	return keys
}

func helpCommandStem(stem string) string {
	fields, ok := splitCommandFields(stem)
	if !ok || len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "./bin/zv", `.\bin\zv.exe`, "zv":
		fields[0] = "zv"
	default:
		return ""
	}
	return strings.Join(fields, " ")
}

func workflowNames(workflows []workflowInfo) []string {
	names := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		names = append(names, workflow.Name)
	}
	return names
}

func workflowForDirectCommand(command []string) (workflowInfo, bool) {
	var matched workflowInfo
	var matchedLen int
	for _, workflow := range workflowCatalog() {
		if len(workflow.RunArgs) <= matchedLen {
			continue
		}
		if isExecutableDirectWorkflowCommand(command, workflow) {
			matched = workflow
			matchedLen = len(workflow.RunArgs)
		}
	}
	return matched, matchedLen > 0
}

func workflowDirectDocCommandIsComparable(workflow workflowInfo) bool {
	return workflowDelegatesExternally(workflow) || workflow.Name == "gallery-open"
}

func workflowRunArgsForDirectCommand(t *testing.T, workflow workflowInfo, directArgs []string) []string {
	t.Helper()
	if len(directArgs) < len(workflow.RunArgs) {
		t.Fatalf("direct args %#v shorter than workflow run args %#v", directArgs, workflow.RunArgs)
	}
	for i, arg := range workflow.RunArgs {
		if directArgs[i] != arg {
			t.Fatalf("direct args %#v do not match workflow run args %#v", directArgs, workflow.RunArgs)
		}
	}
	runArgs := workflowRunCommandArgs(t, workflow)
	forwarded := directArgs[len(workflow.RunArgs):]
	if len(forwarded) > 0 {
		runArgs = append(runArgs, "--")
		runArgs = append(runArgs, forwarded...)
	}
	return runArgs
}

func directArgsForWorkflowRunDocCommand(t *testing.T, workflow workflowInfo, runArgs []string) []string {
	t.Helper()
	if len(runArgs) < 3 || runArgs[0] != "workflows" || runArgs[1] != "run" || runArgs[2] != workflow.Name {
		t.Fatalf("workflow run args %#v do not target workflow %q", runArgs, workflow.Name)
	}
	directArgs := append([]string(nil), workflow.RunArgs...)
	if len(runArgs) == 3 {
		return directArgs
	}
	if runArgs[3] != "--" {
		t.Fatalf(`workflow run args %#v are missing "--" separator before forwarded args`, runArgs)
	}
	return append(directArgs, runArgs[4:]...)
}

func assertDiscoveredWorkflowRunMatchesDirect(t *testing.T, exe, root, source string, index int, discovered workflowInfo, catalogWorkflow workflowInfo, galleryPath string) {
	t.Helper()
	runArgs := workflowRunCommandArgs(t, discovered)
	if len(runArgs) < 3 || runArgs[2] != catalogWorkflow.Name {
		t.Fatalf("%s discovered run_command for %s resolved to args %#v", source, catalogWorkflow.Name, runArgs)
	}
	runArgs = append(runArgs, workflowRunSampleForwardedArgs(t, catalogWorkflow, galleryPath)...)
	directArgs := workflowDirectSampleArgs(t, catalogWorkflow, galleryPath)

	prefix := fmt.Sprintf("%02d-%s-%s", index, source, catalogWorkflow.Name)
	runSubcommandLog := filepath.Join(root, prefix+"-discovered-run.jsonl")
	directSubcommandLog := filepath.Join(root, prefix+"-direct.jsonl")
	runOpenLog := filepath.Join(root, prefix+"-discovered-run-open.txt")
	directOpenLog := filepath.Join(root, prefix+"-direct-open.txt")

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
		t.Fatalf("%s discovered run_command output = %q, want direct output %q", source, got, want)
	}
	if catalogWorkflow.Name == "gallery-open" {
		if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
			t.Fatalf("%s discovered run_command open path log = %q, want direct log %q", source, got, want)
		}
		return
	}

	runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
	directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
	if got, want := len(runCalls), 1; got != want {
		t.Fatalf("%s discovered run_command calls len = %d, want %d: %#v", source, got, want, runCalls)
	}
	if got, want := len(directCalls), 1; got != want {
		t.Fatalf("%s direct calls len = %d, want %d: %#v", source, got, want, directCalls)
	}
	if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
		t.Fatalf("%s discovered run_command executable = %q, want direct executable %q", source, got, want)
	}
	if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
		t.Fatalf("%s discovered run_command args = %#v, want direct args %#v", source, runCalls[0].Args, directCalls[0].Args)
	}
}

func workflowDelegatesExternally(workflow workflowInfo) bool {
	if len(workflow.RunArgs) == 0 {
		return false
	}
	switch workflow.RunArgs[0] {
	case "check", "gallery", "short", "skills", "workflows":
		return false
	default:
		return true
	}
}

func workflowHelpDelegatesExternally(workflow workflowInfo) bool {
	if workflow.Name == "serve" {
		return false
	}
	return workflowDelegatesExternally(workflow)
}

type codexPromptWrapperFixture struct {
	wrapper string
	prompt  string
}

func agentPromptWrapperFixtures() []string {
	var out []string
	for _, fixture := range codexPromptWrapperFixtures() {
		out = append(out, fixture.wrapper)
	}
	for _, fixture := range claudePromptWrapperFixtures() {
		out = append(out, fixture.wrapper)
	}
	return out
}

func codexPromptWrapperFixtures() []codexPromptWrapperFixture {
	return []codexPromptWrapperFixture{
		{wrapper: "scripts/codex-go-bugfix.sh", prompt: ".codex/prompts/go-bugfix.md"},
		{wrapper: "scripts/codex-go-concurrency-review.sh", prompt: ".codex/prompts/go-concurrency-review.md"},
		{wrapper: "scripts/codex-go-pr-ready.sh", prompt: ".codex/prompts/go-pr-ready.md"},
		{wrapper: "scripts/codex-go-readability-review.sh", prompt: ".codex/prompts/go-readability-review.md"},
		{wrapper: "scripts/codex-go-security-review.sh", prompt: ".codex/prompts/go-security-review.md"},
		{wrapper: "scripts/codex-go-tdd.sh", prompt: ".codex/prompts/go-tdd.md"},
		{wrapper: "scripts/codex-go-test-review.sh", prompt: ".codex/prompts/go-test-review.md"},
		{wrapper: "scripts/codex-plan.sh", prompt: ".codex/prompts/go-plan.md"},
		{wrapper: "scripts/codex-review-diff.sh", prompt: ".codex/prompts/review-diff.md"},
		{wrapper: "scripts/codex-spike.sh", prompt: ".codex/prompts/go-spike.md"},
	}
}

type claudePromptWrapperFixture struct {
	wrapper string
	command string
}

func claudePromptWrapperFixtures() []claudePromptWrapperFixture {
	return []claudePromptWrapperFixture{
		{wrapper: "scripts/claude-zv-artifact-audit.sh", command: ".claude/commands/zv-artifact-audit.md"},
		{wrapper: "scripts/claude-zv-bugfix.sh", command: ".claude/commands/zv-bugfix.md"},
		{wrapper: "scripts/claude-zv-media-change.sh", command: ".claude/commands/zv-media-change.md"},
		{wrapper: "scripts/claude-zv-parser-change.sh", command: ".claude/commands/zv-parser-change.md"},
		{wrapper: "scripts/claude-zv-plan.sh", command: ".claude/commands/zv-plan.md"},
		{wrapper: "scripts/claude-zv-pr-ready.sh", command: ".claude/commands/zv-pr-ready.md"},
		{wrapper: "scripts/claude-zv-tdd.sh", command: ".claude/commands/zv-tdd.md"},
		{wrapper: "scripts/claude-zv-toolchain-diagnose.sh", command: ".claude/commands/zv-toolchain-diagnose.md"},
		{wrapper: "scripts/claude-zv-worker-api-change.sh", command: ".claude/commands/zv-worker-api-change.md"},
	}
}

type claudeReviewerAgentFixture struct {
	path string
	body string
}

func claudeReviewerAgentFixtures() []claudeReviewerAgentFixture {
	names := []string{
		"go-readability-reviewer",
		"go-test-reviewer",
		"go-concurrency-reviewer",
		"go-security-reviewer",
		"zv-media-pipeline-reviewer",
	}
	fixtures := make([]claudeReviewerAgentFixture, 0, len(names))
	for _, name := range names {
		extra := ""
		switch name {
		case "go-concurrency-reviewer":
			extra = "\nRecommend `scripts/go-gate.sh --race` when shared state changed.\n"
		case "go-security-reviewer":
			extra = "\nDo not read `.env`, private keys, or token files.\n"
		case "zv-media-pipeline-reviewer":
			extra = "\nAvoid tests that require real HLAE/CS2/large media unless explicitly requested.\n"
		}
		fixtures = append(fixtures, claudeReviewerAgentFixture{
			path: ".claude/agents/" + name + ".md",
			body: strings.Join([]string{
				"---",
				"name: " + name,
				"description: Review " + name,
				"model: sonnet",
				"tools: [Read, Bash]",
				"---",
				"",
				"Use `BLOCKER`, `WARNING`, and `NIT`.",
				"Every finding needs file/path, problem, why it matters, and a practical fix.",
				"If clean, say `No blocking issues found.`.",
				extra,
			}, "\n"),
		})
	}
	return fixtures
}

func codexPromptFixtureBody(prompt string) string {
	switch prompt {
	case ".codex/prompts/go-tdd.md", ".codex/prompts/go-bugfix.md":
		return strings.Join([]string{
			"# Prompt",
			"",
			"Run `scripts/go-gate.sh --no-format` so tests, vet, `zv check`, and static analysis share the project contract.",
			"If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.",
			"",
		}, "\n")
	case ".codex/prompts/go-pr-ready.md":
		return strings.Join([]string{
			"# Prompt",
			"",
			"Run `scripts/go-gate.sh`; use `scripts/go-gate.sh --no-format` in dirty repos.",
			"If concurrency changed, run `scripts/go-gate.sh --race`.",
			"If security changed, run `scripts/go-gate.sh --security`.",
			"",
		}, "\n")
	case ".codex/prompts/go-concurrency-review.md":
		return "# Prompt\n\nRecommend `scripts/go-gate.sh --race --no-format`.\n"
	case ".codex/prompts/go-security-review.md":
		return "# Prompt\n\nRecommend `scripts/go-gate.sh --security`.\n"
	default:
		return "# " + prompt + "\n"
	}
}

func writeSkill(t *testing.T, root, name, description string) {
	t.Helper()
	writeSkillBody(t, root, name, strings.Join([]string{
		"---",
		"name: " + name,
		`description: "` + description + `"`,
		"---",
		"",
		"# " + name,
		"",
		"Workflow details.",
		"",
	}, "\n"))
}

func writeSkillBody(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, ".codex", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func writeWorkflowDocs(t *testing.T, root string) {
	t.Helper()
	catalogDoc := strings.Join([]string{
		"# FragForge",
		"",
		"```bash",
		"./bin/zv presets",
		"./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv demo players --demo testdata/foo.dem",
		"./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv music analyze --input data/music/track.mp4 --out data/runs/run-004/rhythm.json",
		"./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv gallery open --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv check",
		"./bin/zv check --format json",
		"./bin/zv serve",
		"./bin/zv skills check",
		"./bin/zv skills list --format json",
		"./bin/zv skills show alpha",
		"./bin/zv skills show alpha --format json",
		"./bin/zv skills check --format json",
		"alpha",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format json",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
		"./bin/zv workflows show utility-audit",
		"./bin/zv workflows show utility-audit --format json",
		"./bin/zv workflows show record",
		"./bin/zv workflows show record --format json",
		"./bin/zv workflows show compose-final",
		"./bin/zv workflows show compose-final --format json",
		"./bin/zv workflows show music-analyze",
		"./bin/zv workflows show music-analyze --format json",
		"./bin/zv workflows show shorts-render",
		"./bin/zv workflows show shorts-render --format json",
		"./bin/zv workflows show analysis-tactical-data",
		"./bin/zv workflows show analysis-tactical-data --format json",
		"./bin/zv workflows show analysis-viewer",
		"./bin/zv workflows show analysis-viewer --format json",
		"./bin/zv workflows show gallery-open",
		"./bin/zv workflows show gallery-open --format json",
		"./bin/zv workflows show serve",
		"./bin/zv workflows show serve --format json",
		"./bin/zv workflows show skills-check",
		"./bin/zv workflows show skills-check --format json",
		"./bin/zv workflows show workflows-check",
		"./bin/zv workflows show workflows-check --format json",
		"./bin/zv workflows show project-check",
		"./bin/zv workflows show project-check --format json",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
		"./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv workflows run music-analyze -- --input data/music/track.mp4 --out data/runs/run-004/rhythm.json",
		"./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv workflows run serve",
		"./bin/zv workflows run skills-check",
		"./bin/zv workflows run skills-check -- --format json",
		"./bin/zv workflows run workflows-check",
		"./bin/zv workflows run workflows-check -- --format json",
		"./bin/zv workflows run project-check",
		"./bin/zv workflows run project-check -- --format json",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format json",
		"```",
		"",
	}, "\n")
	writeFile(t, filepath.Join(root, "README.md"), catalogDoc)
	writeFile(t, filepath.Join(root, "docs", "workflows", "catalog.md"), catalogDoc)
	writeFile(t, filepath.Join(root, "docs", "toolchain.md"), strings.Join([]string{
		"# Toolchain",
		"",
		"`zv check` validates the unified CLI contract.",
		"",
		"```powershell",
		`.\bin\zv.exe record --killplan plan.json --demo demo.dem --out recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"`,
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "docs", "README.md"), strings.Join([]string{
		"# Docs",
		"",
		"```bash",
		"./bin/zv check",
		"./bin/zv skills list",
		"./bin/zv workflows list",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "docs", "workflows", "zv-short.md"), strings.Join([]string{
		"# zv short",
		"",
		"```bash",
		`./bin/zv short testdata/foo.dem --prompt "todas las kills de martinez" --target-steamid 76561198000000000 --dry-run`,
		`./bin/zv short --prompt "todas las kills" --from-recording data/runs/run-004/recording/recording-result.json --dry-run`,
		"./bin/zv presets",
		"./bin/zv presets --format json",
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "smoke-real.ps1"), strings.Join([]string{
		`Fail "Orchestrator is not reachable. Start bin\zv serve with the current environment and run migrations first."`,
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "smoke.sh"), strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`BASE="${ZV_BASE_URL:-http://localhost:8080}"`,
		`curl -fsS "$BASE/api/jobs"`,
		`curl -fsS "$BASE/api/jobs/$ID"`,
		`curl -fsS "$BASE/api/jobs/$ID/plan"`,
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "Makefile"), strings.Join([]string{
		"build:",
		"\tgo build -o bin/zv ./cmd/zv",
		"",
		"test:",
		"\tgo test ./... -count=1",
		"\tgo run ./cmd/zv check",
		"\tgo run ./cmd/zv workflows check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "build.ps1"), strings.Join([]string{
		`$commands = @(`,
		`    "zv",`,
		`)`,
		`foreach ($name in $commands) {`,
		`    $out = Join-Path $binDir "$name.exe"`,
		`    $pkg = "./cmd/$name"`,
		`    & go build -o $out $pkg`,
		`}`,
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "go-gate.sh"), strings.Join([]string{
		`echo "== zv check =="`,
		"go run ./cmd/zv check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "fix-loop.ps1"), strings.Join([]string{
		`Invoke-Step "zv check" {`,
		"    & go run ./cmd/zv check",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "check-codex-harness.sh"), strings.Join([]string{
		"mapfile -t shell_scripts < <(find scripts -maxdepth 1 -type f -name '*.sh' | sort)",
		`bash -n "${shell_scripts[@]}"`,
		`echo "== FragForge workflow contract =="`,
		"go run ./cmd/zv check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "codex-run.sh"), strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`prompt_file="$1"`,
		`shift || true`,
		`exec codex --cd "$(git rev-parse --show-toplevel)" exec - < "$prompt_file"`,
		"",
	}, "\n"))
	for _, fixture := range codexPromptWrapperFixtures() {
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.prompt)), codexPromptFixtureBody(fixture.prompt))
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.wrapper)), strings.Join([]string{
			"#!/usr/bin/env bash",
			"set -euo pipefail",
			`root="$(git rev-parse --show-toplevel)"`,
			fmt.Sprintf(`exec "$root/scripts/codex-run.sh" %s "$@"`, fixture.prompt),
			"",
		}, "\n"))
	}
	writeFile(t, filepath.Join(root, ".codex", "README.md"), strings.Join([]string{
		"# Codex",
		"",
		"```bash",
		"scripts/codex-run.sh",
		"scripts/codex-plan.sh",
		"scripts/codex-go-tdd.sh",
		"scripts/codex-go-bugfix.sh",
		"scripts/codex-go-pr-ready.sh",
		"scripts/codex-review-diff.sh",
		"scripts/codex-spike.sh",
		"scripts/codex-go-readability-review.sh",
		"scripts/codex-go-test-review.sh",
		"scripts/codex-go-concurrency-review.sh",
		"scripts/codex-go-security-review.sh",
		"```",
		"",
		"```bash",
		"./bin/zv skills list",
		"./bin/zv skills show alpha",
		"./bin/zv skills check",
		"alpha",
		"./bin/zv check",
		"./bin/zv check --format json",
		"./bin/zv skills list --format json",
		"./bin/zv skills show alpha --format json",
		"./bin/zv skills check --format json",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format json",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
		"./bin/zv workflows show utility-audit",
		"./bin/zv workflows show utility-audit --format json",
		"./bin/zv workflows show record",
		"./bin/zv workflows show record --format json",
		"./bin/zv workflows show compose-final",
		"./bin/zv workflows show compose-final --format json",
		"./bin/zv workflows show music-analyze",
		"./bin/zv workflows show music-analyze --format json",
		"./bin/zv workflows show shorts-render",
		"./bin/zv workflows show shorts-render --format json",
		"./bin/zv workflows show analysis-tactical-data",
		"./bin/zv workflows show analysis-tactical-data --format json",
		"./bin/zv workflows show analysis-viewer",
		"./bin/zv workflows show analysis-viewer --format json",
		"./bin/zv workflows show gallery-open",
		"./bin/zv workflows show gallery-open --format json",
		"./bin/zv workflows show serve",
		"./bin/zv workflows show serve --format json",
		"./bin/zv workflows show skills-check",
		"./bin/zv workflows show skills-check --format json",
		"./bin/zv workflows show workflows-check",
		"./bin/zv workflows show workflows-check --format json",
		"./bin/zv workflows show project-check",
		"./bin/zv workflows show project-check --format json",
		"./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv demo players --demo testdata/foo.dem",
		"./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv music analyze --input data/music/track.mp4 --out data/runs/run-004/rhythm.json",
		"./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv gallery open --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv serve",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
		"./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv workflows run music-analyze -- --input data/music/track.mp4 --out data/runs/run-004/rhythm.json",
		"./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv workflows run serve",
		"./bin/zv workflows run skills-check",
		"./bin/zv workflows run skills-check -- --format json",
		"./bin/zv workflows run workflows-check",
		"./bin/zv workflows run workflows-check -- --format json",
		"./bin/zv workflows run project-check",
		"./bin/zv workflows run project-check -- --format json",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format json",
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "AGENTS.md"), strings.Join([]string{
		"# Agents",
		"",
		"```bash",
		`CODEX_DRY_RUN=1 scripts/codex-run.sh .codex/prompts/go-tdd.md "custom prompt run"`,
		`C:\HLAE-2.190.1\HLAE.exe`,
		`C:\HLAE\HLAE.exe`,
		`scripts/codex-go-tdd.sh "implement a behavior change"`,
		`scripts/codex-go-bugfix.sh "fix a bug with a regression test"`,
		`scripts/codex-go-pr-ready.sh`,
		`scripts/go-gate.sh --no-format`,
		`scripts/go-gate.sh --race`,
		`scripts/go-gate.sh --security`,
		"```",
		"",
	}, "\n"))
	for _, fixture := range claudePromptWrapperFixtures() {
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.command)), claudeCommandFixtureBody(fixture.command))
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.wrapper)), strings.Join([]string{
			"#!/usr/bin/env bash",
			"set -euo pipefail",
			`root="$(git rev-parse --show-toplevel)"`,
			fmt.Sprintf(`exec "$root/scripts/claude-run.sh" %s "$@"`, fixture.command),
			"",
		}, "\n"))
	}
	writeFile(t, filepath.Join(root, "scripts", "claude-run.sh"), strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`prompt_file="$1"`,
		`shift || true`,
		`exec claude -p "$(cat "$prompt_file")" "$@"`,
		"",
	}, "\n"))
	claudeReadmeLines := []string{
		"# Claude",
		"",
		"Style and operational rules live in CLAUDE.md.",
		"",
		"```bash",
		"scripts/claude-run.sh",
	}
	for _, fixture := range claudePromptWrapperFixtures() {
		claudeReadmeLines = append(claudeReadmeLines, fixture.wrapper)
	}
	claudeReadmeLines = append(claudeReadmeLines,
		"```",
		"",
		"```text",
		"@go-readability-reviewer review the current diff",
		"@go-test-reviewer review the tests in this diff",
		"@go-concurrency-reviewer review shared-state changes",
		"@go-security-reviewer review filesystem/subprocess/security changes",
		"@zv-media-pipeline-reviewer review FFmpeg/rendering changes",
		"```",
		"",
	)
	writeFile(t, filepath.Join(root, ".claude", "README.md"), strings.Join(claudeReadmeLines, "\n"))
	writeFile(t, filepath.Join(root, "CLAUDE.md"), strings.Join([]string{
		"# Claude",
		"",
		"Write boring, idiomatic Go.",
		"Do not introduce `util`, `common`, `helper`, `manager`, or vague service layers.",
		"Add useful context when returning errors.",
		"Every goroutine must have a clear owner and stop condition.",
		"",
		"Do not add generated video/audio/image artifacts to git.",
		"",
		"```bash",
		`CLAUDE_DRY_RUN=1 scripts/claude-run.sh .claude/commands/zv-tdd.md "custom prompt run"`,
		`C:\HLAE-2.190.1\HLAE.exe`,
		`C:\HLAE\HLAE.exe`,
		`scripts/claude-zv-tdd.sh "implement a behavior change"`,
		`scripts/claude-zv-bugfix.sh "fix a bug with a regression test"`,
		`scripts/claude-zv-pr-ready.sh`,
		`scripts/go-gate.sh --no-format`,
		`scripts/go-gate.sh --race`,
		`scripts/go-gate.sh --security`,
		"```",
		"",
		"```text",
		"@go-readability-reviewer review the current diff",
		"@go-test-reviewer review the tests in this diff",
		"@go-concurrency-reviewer review shared-state changes",
		"@go-security-reviewer review filesystem/subprocess/security changes",
		"@zv-media-pipeline-reviewer review FFmpeg/rendering changes",
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "web", "CLAUDE.md"), strings.Join([]string{
		"# web/ frontend guidance",
		"",
		"## TypeScript style (web/)",
		"",
		"No `any`, ever: use `unknown` and narrow it.",
		"No re-exports: update every import when moving code.",
		"",
	}, "\n"))
	for _, fixture := range claudeReviewerAgentFixtures() {
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.path)), fixture.body)
	}
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), claudeSettingsFixture())
}

func claudeCommandFixtureBody(command string) string {
	switch command {
	case ".claude/commands/zv-plan.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Read-only. Do not edit files.",
			"Run `git status --short`.",
			"",
			"Output:",
			"",
			"- Tests and verification",
			"- Risks / open questions",
			"",
		}, "\n")
	case ".claude/commands/zv-tdd.md", ".claude/commands/zv-bugfix.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run `scripts/go-gate.sh --no-format` so tests, vet, `zv check`, and static analysis share the project contract.",
			"If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.",
			"",
		}, "\n")
	case ".claude/commands/zv-parser-change.md", ".claude/commands/zv-media-change.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run targeted package tests first.",
			"If broad, run `scripts/go-gate.sh --no-format`; it includes `zv check`.",
			"",
		}, "\n")
	case ".claude/commands/zv-worker-api-change.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run targeted package tests first.",
			"If broad, run `scripts/go-gate.sh --no-format`; it includes `zv check`.",
			"If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.",
			"",
		}, "\n")
	case ".claude/commands/zv-pr-ready.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run `scripts/go-gate.sh --no-format`; it includes `zv check`.",
			"If concurrency changed, run `scripts/go-gate.sh --race`.",
			"If security changed, run `scripts/go-gate.sh --security`.",
			"",
		}, "\n")
	case ".claude/commands/zv-artifact-audit.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Read-only. Do not edit or delete files.",
			"Run `git status --short`.",
			"Inspect `.gitignore`.",
			"Check generated run data under `data/`.",
			"Output Suggested commands for manual cleanup only.",
			"",
		}, "\n")
	case ".claude/commands/zv-toolchain-diagnose.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Read-only diagnosis. Do not install tools or edit files unless the user asks.",
			"Run `scripts/go-tools-check.sh`.",
			"Inspect `scripts/check-toolchain.ps1`.",
			"Do not run CS2/HLAE, Docker compose, migrations, or renders.",
			"Output Exact next commands.",
			"",
		}, "\n")
	default:
		return "# " + command + "\n"
	}
}

func claudeSettingsFixture() string {
	return strings.Join([]string{
		"{",
		`  "permissions": {`,
		`    "allow": [`,
		`      "Read",`,
		`      "Edit",`,
		`      "Write",`,
		`      "WebSearch",`,
		`      "WebFetch",`,
		`      "Bash(git status*)",`,
		`      "Bash(git diff*)",`,
		`      "Bash(git log*)",`,
		`      "Bash(go test*)",`,
		`      "Bash(go vet*)",`,
		`      "Bash(gofmt*)",`,
		`      "Bash(goimports*)",`,
		`      "Bash(staticcheck*)",`,
		`      "Bash(govulncheck*)",`,
		`      "Bash(gosec*)",`,
		`      "Bash(scripts/go-format-changed.sh*)",`,
		`      "Bash(scripts/go-gate.sh*)",`,
		`      "Bash(scripts/go-tools-check.sh*)",`,
		`      "Bash(scripts/check-codex-harness.sh*)",`,
		`      "Bash(powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/check-toolchain.ps1*)",`,
		`      "Bash(pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/check-toolchain.ps1*)"`,
		`    ],`,
		`    "ask": [`,
		`      "Bash(go mod tidy*)",`,
		`      "Bash(go get*)",`,
		`      "Bash(go install*)",`,
		`      "Bash(git commit*)",`,
		`      "Bash(git push*)",`,
		`      "Bash(git reset*)",`,
		`      "Bash(git clean*)",`,
		`      "Bash(docker*)",`,
		`      "Bash(docker compose*)",`,
		`      "Bash(ffmpeg*)",`,
		`      "Bash(powershell.exe*)",`,
		`      "Bash(pwsh*)",`,
		`      "Bash(scripts/build.ps1*)",`,
		`      "Bash(scripts/cleanup-artifacts.ps1*)",`,
		`      "Bash(scripts/audit-security-performance.ps1*)"`,
		`    ],`,
		`    "deny": [`,
		`      "Read(.env)",`,
		`      "Read(**/.env)",`,
		`      "Read(**/*id_rsa*)",`,
		`      "Read(**/*id_ed25519*)",`,
		`      "Read(**/*secret*)",`,
		`      "Read(**/*token*)",`,
		`      "Bash(rm -rf *)",`,
		`      "Bash(git reset --hard*)",`,
		`      "Bash(git push --force*)"`,
		`    ]`,
		`  }`,
		"}",
		"",
	}, "\n")
}
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func appendFile(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open append %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func hasIssue(issues []skillIssue, want string) bool {
	for _, issue := range issues {
		if issue.Path+": "+issue.Message == want {
			return true
		}
	}
	return false
}

func hasIssueContaining(issues []skillIssue, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Path+": "+issue.Message, want) {
			return true
		}
	}
	return false
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
}

var (
	cachedZVBinaryOnce   sync.Once
	cachedZVBinaryDir    string
	cachedZVBinaryPath   string
	cachedZVBinaryOutput []byte
	cachedZVBinaryErr    error
)

func buildZVBinary(t *testing.T, _ string) string {
	t.Helper()
	// Every binary E2E test calls this helper. The sole E2E test that mutates
	// process-wide environment and working-directory state deliberately does
	// not, so these expensive subprocess tests can safely run concurrently.
	t.Parallel()
	cachedZVBinaryOnce.Do(func() {
		cachedZVBinaryDir, cachedZVBinaryErr = os.MkdirTemp("", "zv-test-bin-*")
		if cachedZVBinaryErr != nil {
			return
		}
		cachedZVBinaryPath = filepath.Join(cachedZVBinaryDir, executableName("zv"))
		cmd := exec.Command("go", "build", "-o", cachedZVBinaryPath, ".")
		cachedZVBinaryOutput, cachedZVBinaryErr = cmd.CombinedOutput()
	})
	if cachedZVBinaryErr != nil {
		t.Fatalf("go build ./cmd/zv: %v\n%s", cachedZVBinaryErr, cachedZVBinaryOutput)
	}

	testBinDir, err := os.MkdirTemp(cachedZVBinaryDir, "case-*")
	if err != nil {
		t.Fatalf("create test binary directory: %v", err)
	}
	exe := filepath.Join(testBinDir, executableName("zv"))
	// A hardlink gives tests the private path needed for adjacent fake
	// subcommands without rewriting and rescanning the 40 MB executable for
	// every case on Windows. Fall back for filesystems without hardlinks.
	if err := os.Link(cachedZVBinaryPath, exe); err != nil {
		copyFile(t, cachedZVBinaryPath, exe)
	}
	return exe
}

func removeAllTestArtifacts(path string) error {
	if path == "" {
		return nil
	}
	var err error
	for attempt := 0; attempt < 40; attempt++ {
		err = os.RemoveAll(path)
		if err == nil {
			return nil
		}
		if runtime.GOOS != "windows" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

func runZVBinary(t *testing.T, exe, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", exe, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runZVBinarySplit(t *testing.T, exe, dir string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runZVBinarySplitWithEnv(t *testing.T, exe, dir string, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runZVBinarySplitWithEnvAndInput(t *testing.T, exe, dir string, env []string, input string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runZVBinaryFailure(t *testing.T, exe, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s %s succeeded unexpectedly\n%s", exe, strings.Join(args, " "), out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("%s %s failed without exit code: %v\n%s", exe, strings.Join(args, " "), err, out)
	}
	return string(out), exitErr.ExitCode()
}

func runZVBinaryFailureSplit(t *testing.T, exe, dir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("%s %s succeeded unexpectedly\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), stdout.String(), stderr.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("%s %s failed without exit code: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String(), exitErr.ExitCode()
}

func runZVBinaryFailureSplitWithEnv(t *testing.T, exe, dir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("%s %s succeeded unexpectedly\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), stdout.String(), stderr.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("%s %s failed without exit code: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String(), exitErr.ExitCode()
}

func runAgentWrapperDryRun(t *testing.T, root, wrapper string, env []string, task string) string {
	t.Helper()
	var script strings.Builder
	script.WriteString("set -euo pipefail\n")
	for _, item := range env {
		script.WriteString("export ")
		script.WriteString(item)
		script.WriteString("\n")
	}
	script.WriteString("bash ")
	script.WriteString(shellQuote(filepath.ToSlash(wrapper)))
	script.WriteString(" ")
	script.WriteString(shellQuote(task))
	cmd := exec.Command("bash", "-c", script.String())
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s dry run failed: %v\n%s", wrapper, err, out)
	}
	return string(out)
}

func runAgentRunnerDryRunWithInput(t *testing.T, root, env, runner, prompt, task, input string) (string, string) {
	t.Helper()
	cmd := exec.Command("bash", "-c", env+" "+shellQuote(runner)+" "+shellQuote(prompt)+" "+shellQuote(task))
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s dry run failed: %v\nstdout:\n%s\nstderr:\n%s", runner, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stripHostShellWarnings(stderr.String())
}

func stripHostShellWarnings(stderr string) string {
	lines := strings.Split(stderr, "\n")
	out := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "wsl: Failed to mount ") && strings.HasSuffix(trimmed, "see dmesg for more details.") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func normalizedText(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

// bashPath converts a Windows path to the POSIX form a spawned "bash" expects
// on PATH. Which convention applies depends on which bash actually runs the
// command: Git Bash/MSYS (the default on GitHub-hosted windows-latest
// runners, and this project's supported bash per CLAUDE.md) mounts drives at
// "/c/...", while WSL mounts them at "/mnt/c/...". Return both, colon-joined,
// so the caller's PATH prefix works under either — same defensive pattern as
// scripts/go-env.sh's candidate list.
func bashPath(path string) string {
	path = filepath.ToSlash(path)
	if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
		drive := strings.ToLower(path[:1])
		rest := path[3:]
		return "/" + drive + "/" + rest + ":/mnt/" + drive + "/" + rest
	}
	return path
}

func findPowerShell() (string, bool) {
	for _, name := range []string{"pwsh", "powershell.exe", "powershell"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, true
		}
	}
	return "", false
}

func runZVBinaryWithEnv(t *testing.T, exe, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", exe, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func assertPathDoesNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists, want no file", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
}
