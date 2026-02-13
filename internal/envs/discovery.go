package envs

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/justinholmes/grunter/internal/config"
)

// DiscoverUnits walks the environment's path and returns all Terragrunt units
// found underneath it. The LogicalName for each unit has the env path prefix
// stripped (e.g. "envs/dev/us-east-1/vpc" → "us-east-1/vpc").
func DiscoverUnits(env config.Environment) ([]UnitInventory, error) {
	var units []UnitInventory

	root := env.Path
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".terraform" || base == ".terragrunt-cache" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == "terragrunt.hcl" {
			dir := filepath.Dir(path)
			// Skip root-level terragrunt.hcl (usually a root config)
			if dir == root {
				return nil
			}

			rel, relErr := filepath.Rel(root, dir)
			if relErr != nil {
				return relErr
			}

			region := extractRegion(rel, env.Regions)
			units = append(units, UnitInventory{
				LogicalName: rel,
				FullPath:    dir,
				Region:      region,
			})
		}
		return nil
	})
	return units, err
}

// extractRegion checks if the first path segment matches a configured region.
func extractRegion(logicalName string, regions []string) string {
	first := logicalName
	if idx := strings.IndexByte(logicalName, filepath.Separator); idx >= 0 {
		first = logicalName[:idx]
	}
	// Also handle forward slash on all platforms
	if idx := strings.IndexByte(first, '/'); idx >= 0 {
		first = first[:idx]
	}
	for _, r := range regions {
		if first == r {
			return r
		}
	}
	return ""
}
