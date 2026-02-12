package changes

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Detector analyzes git diffs to find changed Terragrunt units.
type Detector struct {
	ignorePatterns []string
	// GitDiffFunc can be overridden for testing.
	GitDiffFunc func(base, head string) ([]string, error)
}

// NewDetector creates a Detector with the given ignore patterns.
func NewDetector(ignorePatterns []string) *Detector {
	d := &Detector{ignorePatterns: ignorePatterns}
	d.GitDiffFunc = d.gitDiff
	return d
}

// Detect finds all changed Terragrunt units between base and head commits.
func (d *Detector) Detect(base, head string) ([]Change, error) {
	files, err := d.GitDiffFunc(base, head)
	if err != nil {
		return nil, err
	}

	files = d.filterIgnored(files)

	unitChanges := make(map[string]*Change)

	for _, f := range files {
		// Check if this is an _envcommon change
		if isEnvCommon(f) {
			dependents := d.findEnvCommonDependents(f)
			for _, dep := range dependents {
				if existing, ok := unitChanges[dep]; ok {
					existing.Files = append(existing.Files, f)
				} else {
					unitChanges[dep] = &Change{
						UnitPath: dep,
						Type:     EnvCommonChanged,
						Files:    []string{f},
					}
				}
			}
			continue
		}

		unitPath := d.resolveUnit(f)
		if unitPath == "" {
			continue
		}

		if existing, ok := unitChanges[unitPath]; ok {
			existing.Files = append(existing.Files, f)
			continue
		}

		changeType := d.classifyChange(unitPath, f)
		unitChanges[unitPath] = &Change{
			UnitPath: unitPath,
			Type:     changeType,
			Files:    []string{f},
		}
	}

	result := make([]Change, 0, len(unitChanges))
	for _, c := range unitChanges {
		result = append(result, *c)
	}
	return result, nil
}

// gitDiff runs git diff and returns changed file paths.
func (d *Detector) gitDiff(base, head string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", base+".."+head)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// filterIgnored removes files matching any ignore pattern.
func (d *Detector) filterIgnored(files []string) []string {
	if len(d.ignorePatterns) == 0 {
		return files
	}

	var result []string
	for _, f := range files {
		ignored := false
		for _, pattern := range d.ignorePatterns {
			matched, err := filepath.Match(pattern, f)
			if err == nil && matched {
				ignored = true
				break
			}
			// Also try matching just the filename for ** patterns
			if strings.HasPrefix(pattern, "**/") {
				subPattern := strings.TrimPrefix(pattern, "**/")
				matched, err = filepath.Match(subPattern, filepath.Base(f))
				if err == nil && matched {
					ignored = true
					break
				}
			}
		}
		if !ignored {
			result = append(result, f)
		}
	}
	return result
}

// resolveUnit walks up the directory tree from a file to find the nearest terragrunt.hcl.
func (d *Detector) resolveUnit(filePath string) string {
	dir := filepath.Dir(filePath)
	for dir != "." && dir != "/" {
		hclPath := filepath.Join(dir, "terragrunt.hcl")
		if _, err := os.Stat(hclPath); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	// Check root
	if _, err := os.Stat("terragrunt.hcl"); err == nil && dir == "." {
		return "."
	}
	return ""
}

// classifyChange determines the change type for a unit.
func (d *Detector) classifyChange(unitPath, filePath string) ChangeType {
	hclPath := filepath.Join(unitPath, "terragrunt.hcl")

	// If the terragrunt.hcl itself is the changed file, check if it's new or deleted
	if filePath == hclPath {
		if _, err := os.Stat(hclPath); os.IsNotExist(err) {
			return ModuleDeleted
		}
		// Check if it's a new file by seeing if the parent dir is newly created
		// For simplicity, treat it as ModuleChanged; the git status would tell us more
		return ModuleChanged
	}

	// Check if the whole directory is new (ModuleAdded)
	if _, err := os.Stat(hclPath); os.IsNotExist(err) {
		return ModuleAdded
	}

	return ModuleChanged
}

// isEnvCommon returns true if the file is inside an _envcommon directory.
func isEnvCommon(filePath string) bool {
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for _, p := range parts {
		if p == "_envcommon" {
			return true
		}
	}
	return false
}

// findEnvCommonDependents finds Terragrunt units that include the given _envcommon file.
// It searches sibling and nested directories for terragrunt.hcl files that might
// reference the _envcommon path.
func (d *Detector) findEnvCommonDependents(envCommonFile string) []string {
	// Walk up to find the _envcommon directory's parent
	parts := strings.Split(filepath.ToSlash(envCommonFile), "/")
	var envCommonIdx int
	for i, p := range parts {
		if p == "_envcommon" {
			envCommonIdx = i
			break
		}
	}

	// Search from the parent of _envcommon
	searchRoot := "."
	if envCommonIdx > 0 {
		searchRoot = strings.Join(parts[:envCommonIdx], "/")
	}

	var dependents []string
	_ = filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".terraform" || base == ".terragrunt-cache" || base == "_envcommon" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == "terragrunt.hcl" {
			dir := filepath.Dir(path)
			if dir != searchRoot {
				dependents = append(dependents, dir)
			}
		}
		return nil
	})

	return dependents
}
