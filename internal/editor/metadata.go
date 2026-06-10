package editor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rechedev9/fragforge/internal/killplan"
)

type pipelineResultRef struct {
	KillPlan string `json:"killplan"`
}

func resolveKillPlan(recordingResultPath, explicitPath string) (*killplan.Plan, string, []string, error) {
	if explicitPath != "" {
		path, err := filepath.Abs(explicitPath)
		if err != nil {
			return nil, "", nil, fmt.Errorf("resolve killplan path: %w", err)
		}
		plan, err := readKillPlan(path)
		if err != nil {
			return nil, "", nil, err
		}
		return &plan, path, nil, nil
	}

	path, err := discoverKillPlanPath(recordingResultPath)
	if err != nil {
		return nil, "", []string{err.Error()}, nil
	}
	if path == "" {
		return nil, "", nil, nil
	}
	plan, err := readKillPlan(path)
	if err != nil {
		return nil, path, []string{fmt.Sprintf("read auto-discovered killplan %s: %v", path, err)}, nil
	}
	return &plan, path, nil, nil
}

func discoverKillPlanPath(recordingResultPath string) (string, error) {
	recordingDir := filepath.Dir(recordingResultPath)
	runDir := filepath.Dir(recordingDir)
	pipelinePath := filepath.Join(runDir, "pipeline-result.json")
	// #nosec G304 -- pipelinePath is derived from the recording result's local run directory.
	if b, err := os.ReadFile(pipelinePath); err == nil {
		var ref pipelineResultRef
		if err := json.Unmarshal(b, &ref); err != nil {
			return "", fmt.Errorf("parse %s: %v", pipelinePath, err)
		}
		if ref.KillPlan != "" {
			if filepath.IsAbs(ref.KillPlan) {
				return ref.KillPlan, nil
			}
			return filepath.Clean(filepath.Join(runDir, ref.KillPlan)), nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %v", pipelinePath, err)
	}

	parentPlan := filepath.Join(filepath.Dir(runDir), "plan.json")
	if _, err := os.Stat(parentPlan); err == nil {
		return parentPlan, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %v", parentPlan, err)
	}
	return "", nil
}

func readKillPlan(path string) (killplan.Plan, error) {
	// #nosec G304 -- kill plan path is an explicit local CLI/config input.
	b, err := os.ReadFile(path)
	if err != nil {
		return killplan.Plan{}, err
	}
	var plan killplan.Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		return killplan.Plan{}, err
	}
	return plan, nil
}
