package envs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatDiffMarkdown renders an EnvDiffResult as a markdown report.
// Diffs that only differ by environment name are treated as identical
// to reduce noise in the report.
func FormatDiffMarkdown(result *EnvDiffResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Environment Diff: %s → %s\n\n", result.SourceEnv, result.TargetEnv)
	b.WriteString("> **Merging this MR will start deploying to all target environments.**\n\n")

	// Classify diffs, filtering out env-name-only changes
	onlySource := len(result.Structural.OnlyInSource)
	onlyTarget := len(result.Structural.OnlyInTarget)
	var changed, identical int
	var meaningfulDiffs []ContentDiff
	for _, cd := range result.ContentDiffs {
		if cd.Identical || isEnvNameOnlyDiff(cd.Unified, result.SourceEnv, result.TargetEnv) {
			identical++
		} else {
			changed++
			meaningfulDiffs = append(meaningfulDiffs, cd)
		}
	}

	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Metric | Count |\n")
	fmt.Fprintf(&b, "|--------|-------|\n")
	fmt.Fprintf(&b, "| Only in **%s** | %d |\n", result.SourceEnv, onlySource)
	fmt.Fprintf(&b, "| Only in **%s** | %d |\n", result.TargetEnv, onlyTarget)
	fmt.Fprintf(&b, "| Shared — identical | %d |\n", identical)
	fmt.Fprintf(&b, "| Shared — different | %d |\n\n", changed)

	// Structural details
	if onlySource > 0 {
		fmt.Fprintf(&b, "## Units only in %s\n\n", result.SourceEnv)
		for _, u := range result.Structural.OnlyInSource {
			fmt.Fprintf(&b, "- `%s`\n", u.LogicalName)
		}
		b.WriteString("\n")
	}
	if onlyTarget > 0 {
		fmt.Fprintf(&b, "## Units only in %s\n\n", result.TargetEnv)
		for _, u := range result.Structural.OnlyInTarget {
			fmt.Fprintf(&b, "- `%s`\n", u.LogicalName)
		}
		b.WriteString("\n")
	}

	// Content diffs (only meaningful ones)
	if len(meaningfulDiffs) > 0 {
		fmt.Fprintf(&b, "## Content differences\n\n")
		for _, cd := range meaningfulDiffs {
			fmt.Fprintf(&b, "<details open>\n<summary><code>%s</code></summary>\n\n", cd.LogicalName)
			fmt.Fprintf(&b, "```diff\n%s```\n\n", cd.Unified)
			b.WriteString("</details>\n\n")
		}
	}

	return b.String()
}

// isEnvNameOnlyDiff checks whether a unified diff only contains changes where
// the source env name is swapped for the target env name. These expected
// differences (e.g. environment = "staging" vs "production") are noise.
func isEnvNameOnlyDiff(unified, sourceEnv, targetEnv string) bool {
	if unified == "" {
		return true
	}
	for _, line := range strings.Split(unified, "\n") {
		if len(line) == 0 {
			continue
		}
		// Skip diff headers and context lines
		if line[0] != '+' && line[0] != '-' {
			continue
		}
		// Skip --- / +++ file headers
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		// This is a changed line — check if the counterpart exists
		// by normalizing the env name and seeing if the line content
		// would be identical.
		content := line[1:] // strip the +/-
		if line[0] == '-' {
			normalized := strings.ReplaceAll(content, sourceEnv, "<env>")
			// Look for a matching + line in the diff
			counterpart := "+" + strings.ReplaceAll(normalized, "<env>", targetEnv)
			if !strings.Contains(unified, counterpart) {
				return false
			}
		}
	}
	return true
}

// FormatDiffJSON renders an EnvDiffResult as JSON.
func FormatDiffJSON(result *EnvDiffResult) (string, error) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
