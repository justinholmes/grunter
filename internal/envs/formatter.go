package envs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatDiffMarkdown renders an EnvDiffResult as a markdown report.
func FormatDiffMarkdown(result *EnvDiffResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Environment Diff: %s → %s\n\n", result.SourceEnv, result.TargetEnv)

	// Structural summary
	onlySource := len(result.Structural.OnlyInSource)
	onlyTarget := len(result.Structural.OnlyInTarget)
	var changed, identical int
	for _, cd := range result.ContentDiffs {
		if cd.Identical {
			identical++
		} else {
			changed++
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

	// Content diffs
	if changed > 0 {
		fmt.Fprintf(&b, "## Content differences\n\n")
		for _, cd := range result.ContentDiffs {
			if cd.Identical {
				continue
			}
			fmt.Fprintf(&b, "<details open>\n<summary><code>%s</code></summary>\n\n", cd.LogicalName)
			fmt.Fprintf(&b, "```diff\n%s```\n\n", cd.Unified)
			b.WriteString("</details>\n\n")
		}
	}

	return b.String()
}

// FormatDiffJSON renders an EnvDiffResult as JSON.
func FormatDiffJSON(result *EnvDiffResult) (string, error) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
