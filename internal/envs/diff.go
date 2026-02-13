package envs

import (
	"os/exec"
	"path/filepath"

	"github.com/justinholmes/grunter/internal/config"
)

// Diff compares two environments structurally and by content.
func Diff(source, target config.Environment) (*EnvDiffResult, error) {
	sourceUnits, err := DiscoverUnits(source)
	if err != nil {
		return nil, err
	}
	targetUnits, err := DiscoverUnits(target)
	if err != nil {
		return nil, err
	}

	sourceMap := make(map[string]UnitInventory, len(sourceUnits))
	for _, u := range sourceUnits {
		sourceMap[u.LogicalName] = u
	}
	targetMap := make(map[string]UnitInventory, len(targetUnits))
	for _, u := range targetUnits {
		targetMap[u.LogicalName] = u
	}

	result := &EnvDiffResult{
		SourceEnv: source.Name,
		TargetEnv: target.Name,
	}

	// Structural: in source but not target
	for _, u := range sourceUnits {
		if _, ok := targetMap[u.LogicalName]; !ok {
			result.Structural.OnlyInSource = append(result.Structural.OnlyInSource, u)
		}
	}
	// Structural: in target but not source
	for _, u := range targetUnits {
		if _, ok := sourceMap[u.LogicalName]; !ok {
			result.Structural.OnlyInTarget = append(result.Structural.OnlyInTarget, u)
		}
	}

	// Content: shared units
	for _, su := range sourceUnits {
		tu, ok := targetMap[su.LogicalName]
		if !ok {
			continue
		}
		srcFile := filepath.Join(su.FullPath, "terragrunt.hcl")
		tgtFile := filepath.Join(tu.FullPath, "terragrunt.hcl")

		diff, identical := unifiedDiff(srcFile, tgtFile)
		result.ContentDiffs = append(result.ContentDiffs, ContentDiff{
			LogicalName: su.LogicalName,
			SourcePath:  srcFile,
			TargetPath:  tgtFile,
			Unified:     diff,
			Identical:   identical,
		})
	}

	return result, nil
}

// unifiedDiff shells out to diff -u for a unified diff between two files.
func unifiedDiff(fileA, fileB string) (string, bool) {
	cmd := exec.Command("diff", "-u", fileA, fileB)
	out, err := cmd.CombinedOutput()
	if err == nil {
		// exit 0 → files are identical
		return "", true
	}
	// exit 1 → differences found, exit 2 → error (still return output)
	return string(out), false
}
