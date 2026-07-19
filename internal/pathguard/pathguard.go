// Package pathguard prevents local CLI outputs from replacing their source
// artifacts through equal paths, symlinks, hardlinks, or Windows case aliases.
package pathguard

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Input identifies one source artifact and the CLI flag that supplied it.
type Input struct {
	Flag string
	Path string
}

// RejectOutputAliases rejects an output that resolves to any input artifact.
func RejectOutputAliases(output string, inputs ...Input) error {
	outputPath, err := canonical(output)
	if err != nil {
		return fmt.Errorf("resolve --out: %w", err)
	}
	outputInfo, _ := os.Stat(outputPath)
	for _, input := range inputs {
		inputPath, err := canonical(input.Path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", input.Flag, err)
		}
		same := pathsEqual(outputPath, inputPath)
		if !same && outputInfo != nil {
			if inputInfo, statErr := os.Stat(inputPath); statErr == nil {
				same = os.SameFile(outputInfo, inputInfo)
			}
		}
		if same {
			return fmt.Errorf("--out must not overwrite %s %q", input.Flag, inputPath)
		}
	}
	return nil
}

// RejectInputsWithinDirectory rejects source artifacts that a recursive
// replacement of directory would remove.
func RejectInputsWithinDirectory(directory string, inputs ...Input) error {
	directoryPath, err := canonical(directory)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	for _, input := range inputs {
		inputPath, err := canonical(input.Path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", input.Flag, err)
		}
		inside, err := pathWithin(directoryPath, inputPath)
		if err != nil {
			return fmt.Errorf("compare %s with output directory: %w", input.Flag, err)
		}
		if inside {
			return fmt.Errorf("%s %q must not be inside publish directory %q", input.Flag, inputPath, directoryPath)
		}
	}
	return nil
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	}
	parent := filepath.Dir(abs)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolvedParent, filepath.Base(abs)), nil
	}
	return abs, nil
}

func pathsEqual(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathWithin(directory, candidate string) (bool, error) {
	// A path on a different volume cannot be inside directory, and
	// filepath.Rel returns a hard error across volumes on Windows.
	if !strings.EqualFold(filepath.VolumeName(directory), filepath.VolumeName(candidate)) {
		return false, nil
	}
	relative, err := filepath.Rel(directory, candidate)
	if err != nil {
		return false, err
	}
	if relative == "." {
		return true, nil
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative), nil
}
