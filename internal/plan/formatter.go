package plan

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/justinholmes/grunter/internal/runner"
)

const maxCommentSize = 900000 // GitLab limit is ~1MB, leave margin

// Status represents the outcome of a unit's plan/apply.
type Status string

const (
	StatusChanged   Status = "changed"
	StatusNoChanges Status = "no_changes"
	StatusError     Status = "error"
	StatusDrift     Status = "drift"
)

// UnitResult holds the formatted output for a single unit.
type UnitResult struct {
	Path    string
	Status  Status
	Output  string
	Summary *runner.PlanSummary
}

// ansiRe strips ANSI escape codes from output for cleaner markdown rendering.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// tgLogRe matches terragrunt log prefixes like "22:21:11.552 STDOUT tofu: "
var tgLogRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3}\s+\w+\s+\w+:\s*`)

// cleanOutput strips ANSI codes and terragrunt log prefixes from plan/apply output,
// keeping only the meaningful terraform content.
func cleanOutput(output string) string {
	// Strip ANSI escape codes
	cleaned := ansiRe.ReplaceAllString(output, "")

	var lines []string
	inPlan := false
	for _, line := range strings.Split(cleaned, "\n") {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines at the start
		if len(lines) == 0 && trimmed == "" {
			continue
		}

		// Skip terragrunt init/provider noise and terraform boilerplate
		if strings.Contains(trimmed, "Initializing the backend") ||
			strings.Contains(trimmed, "Successfully configured the backend") ||
			strings.Contains(trimmed, "Initializing provider plugins") ||
			strings.Contains(trimmed, "Finding hashicorp/") ||
			strings.Contains(trimmed, "Installing hashicorp/") ||
			strings.Contains(trimmed, "Installed hashicorp/") ||
			strings.Contains(trimmed, "Providers are signed by") ||
			strings.Contains(trimmed, "provider signing") ||
			strings.Contains(trimmed, "opentofu.org/docs/cli/plugins/signing") ||
			strings.Contains(trimmed, "lock file") ||
			strings.Contains(trimmed, "Include this file") ||
			strings.Contains(trimmed, "version control repository") ||
			strings.Contains(trimmed, "OpenTofu has been successfully initialized") ||
			strings.Contains(trimmed, "Terraform has been successfully initialized") ||
			strings.Contains(trimmed, "begin working with OpenTofu") ||
			strings.Contains(trimmed, "begin working with Terraform") ||
			strings.Contains(trimmed, "tofu plan") && !strings.Contains(trimmed, "Plan:") ||
			strings.Contains(trimmed, "tofu init") ||
			strings.Contains(trimmed, "backend configuration changes") ||
			strings.Contains(trimmed, "reinitialize your working directory") ||
			strings.Contains(trimmed, "detect it and remind you") ||
			strings.Contains(trimmed, "same selections by default") ||
			strings.Contains(trimmed, "any changes that are required") ||
			strings.Contains(trimmed, "should now work") ||
			strings.Contains(trimmed, "set or change modules") ||
			strings.Contains(trimmed, "anti-pattern") ||
			strings.Contains(trimmed, "root of Terragrunt configurations") ||
			strings.Contains(trimmed, "root.hcl") ||
			strings.Contains(trimmed, "EXIT:") ||
			strings.Contains(trimmed, "WARN") && strings.Contains(trimmed, "terragrunt.hcl") {
			continue
		}

		// Strip terragrunt log prefix (timestamp STDOUT tofu: )
		stripped := tgLogRe.ReplaceAllString(trimmed, "")

		// Detect plan start
		if strings.Contains(stripped, "used the selected providers") ||
			strings.Contains(stripped, "Refreshing state") {
			inPlan = true
		}

		if inPlan || strings.HasPrefix(stripped, "Plan:") ||
			strings.HasPrefix(stripped, "No changes") ||
			strings.HasPrefix(stripped, "Apply complete") ||
			strings.HasPrefix(stripped, "Changes to Outputs") {
			lines = append(lines, stripped)
		}
	}

	// Trim trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	result := strings.Join(lines, "\n")

	// If filtering removed everything, fall back to the ANSI-stripped
	// output so error messages are still visible.
	if result == "" && strings.TrimSpace(cleaned) != "" {
		return strings.TrimSpace(cleaned)
	}

	return result
}

// FormatComment formats a single unit's plan/apply output as a markdown MR comment.
func FormatComment(unitPath, action, output string) string {
	summary := runner.ParsePlanOutput(output)
	cleaned := cleanOutput(output)

	var sb strings.Builder

	// Header with status icon and unit path
	icon := actionIcon(action, summary)
	sb.WriteString(fmt.Sprintf("### %s `%s` %s\n\n", icon, unitPath, action))

	// Resource change summary with colored badges
	if summary != nil && summary.HasChanges() {
		sb.WriteString(formatChangeBadges(summary))
		sb.WriteString("\n\n")
	} else if summary != nil && summary.NoChanges {
		sb.WriteString("**No changes.** Infrastructure is up-to-date.\n\n")
	}

	// Collapsible plan output
	truncated := truncateOutput(cleaned, maxCommentSize-sb.Len()-200)
	sb.WriteString("<details open>\n")
	sb.WriteString(fmt.Sprintf("<summary>Show full %s output</summary>\n\n", action))
	sb.WriteString("```hcl\n")
	sb.WriteString(truncated)
	sb.WriteString("\n```\n")
	sb.WriteString("</details>\n")

	return sb.String()
}

// FormatMultiUnitComment formats multiple unit results into a single MR comment.
func FormatMultiUnitComment(results []UnitResult) string {
	var sb strings.Builder
	sb.WriteString("## Grunter Plan Results\n\n")

	// Summary totals
	totalAdd, totalChange, totalDestroy := 0, 0, 0
	for _, r := range results {
		if r.Summary != nil {
			totalAdd += r.Summary.ToAdd
			totalChange += r.Summary.ToChange
			totalDestroy += r.Summary.ToDestroy
		}
	}
	if totalAdd > 0 || totalChange > 0 || totalDestroy > 0 {
		sb.WriteString(fmt.Sprintf("**%d** unit(s) with changes", len(results)))
		sb.WriteString(fmt.Sprintf(" &mdash; **+%d** ~%d **-%d** resources\n\n", totalAdd, totalChange, totalDestroy))
	}

	// Summary table
	sb.WriteString("| Status | Unit | Resources |\n")
	sb.WriteString("|:------:|------|----------:|\n")
	for _, r := range results {
		icon := statusIcon(r.Status)
		changes := ""
		if r.Summary != nil && r.Summary.HasChanges() {
			changes = formatCompactChanges(r.Summary)
		} else if r.Summary != nil && r.Summary.NoChanges {
			changes = "No changes"
		}
		sb.WriteString(fmt.Sprintf("| %s | `%s` | %s |\n", icon, r.Path, changes))
	}
	sb.WriteString("\n")

	// Per-unit details
	for _, r := range results {
		cleaned := cleanOutput(r.Output)
		truncated := truncateOutput(cleaned, (maxCommentSize-sb.Len())/len(results))
		sb.WriteString(fmt.Sprintf("<details open>\n<summary>%s <code>%s</code> &mdash; %s</summary>\n\n",
			statusIcon(r.Status), r.Path, formatCompactChanges(r.Summary)))
		sb.WriteString("```hcl\n")
		sb.WriteString(truncated)
		sb.WriteString("\n```\n")
		sb.WriteString("</details>\n\n")
	}

	sb.WriteString("---\n*Posted by [Grunter](https://github.com/justinholmes/grunter)*\n")

	return sb.String()
}

// FormatDriftReport formats drift detection results as markdown.
func FormatDriftReport(results []UnitResult) string {
	var sb strings.Builder
	sb.WriteString("## Drift Detection Report\n\n")
	sb.WriteString(fmt.Sprintf("Found drift in **%d** unit(s):\n\n", len(results)))

	sb.WriteString("| Status | Unit | Details |\n")
	sb.WriteString("|:------:|------|--------:|\n")
	for _, r := range results {
		details := ""
		if r.Summary != nil && r.Summary.HasChanges() {
			details = formatCompactChanges(r.Summary)
		} else if r.Status == StatusError {
			details = "Error running plan"
		}
		sb.WriteString(fmt.Sprintf("| %s | `%s` | %s |\n", statusIcon(r.Status), r.Path, details))
	}
	sb.WriteString("\n")

	for _, r := range results {
		if r.Output == "" {
			continue
		}
		cleaned := cleanOutput(r.Output)
		truncated := truncateOutput(cleaned, 50000)
		sb.WriteString(fmt.Sprintf("<details open>\n<summary><code>%s</code> &mdash; %s</summary>\n\n",
			r.Path, formatCompactChanges(r.Summary)))
		sb.WriteString("```hcl\n")
		sb.WriteString(truncated)
		sb.WriteString("\n```\n")
		sb.WriteString("</details>\n\n")
	}

	sb.WriteString("---\n*Posted by [Grunter](https://github.com/justinholmes/grunter)*\n")

	return sb.String()
}

func actionIcon(action string, summary *runner.PlanSummary) string {
	if summary != nil && summary.NoChanges {
		return ":white_check_mark:"
	}
	if action == "apply" {
		return ":rocket:"
	}
	if summary != nil && summary.ToDestroy > 0 {
		return ":warning:"
	}
	if summary != nil && summary.HasChanges() {
		return ":arrows_counterclockwise:"
	}
	return ":question:"
}

func statusIcon(s Status) string {
	switch s {
	case StatusChanged:
		return ":arrows_counterclockwise:"
	case StatusNoChanges:
		return ":white_check_mark:"
	case StatusError:
		return ":x:"
	case StatusDrift:
		return ":warning:"
	default:
		return ":grey_question:"
	}
}

// formatChangeBadges returns a line like: **+3** to add, **~0** to change, **-1** to destroy
func formatChangeBadges(s *runner.PlanSummary) string {
	if s == nil || s.NoChanges {
		return "No changes"
	}
	parts := []string{}
	if s.ToAdd > 0 {
		parts = append(parts, fmt.Sprintf("**+%d** to add", s.ToAdd))
	}
	if s.ToChange > 0 {
		parts = append(parts, fmt.Sprintf("**~%d** to change", s.ToChange))
	}
	if s.ToDestroy > 0 {
		parts = append(parts, fmt.Sprintf("**-%d** to destroy", s.ToDestroy))
	}
	if len(parts) == 0 {
		return "No changes"
	}
	return strings.Join(parts, " &middot; ")
}

// formatCompactChanges returns a compact "+N ~N -N" style string.
func formatCompactChanges(s *runner.PlanSummary) string {
	if s == nil || s.NoChanges {
		return "No changes"
	}
	if !s.HasChanges() {
		return "No changes"
	}
	return fmt.Sprintf("+%d ~%d -%d", s.ToAdd, s.ToChange, s.ToDestroy)
}

// DeploymentEnv holds information about an environment in the deployment summary.
type DeploymentEnv struct {
	Name       string
	Regions    []string
	UnitCount  int    // >0 means this env has direct changes
	SourceName string // non-empty for promotion targets (e.g. "staging")
}

// FormatDeploymentSummary generates a markdown deployment summary showing
// the full promotion chain. Environments with direct changes show unit counts;
// downstream environments show "promoted from {source}".
func FormatDeploymentSummary(envs []DeploymentEnv) string {
	var sb strings.Builder
	sb.WriteString("## Deployment Summary\n\n")
	sb.WriteString("**Merging this MR will trigger a progressive deployment:**\n\n")

	sb.WriteString("| Step | Environment | Region | Details |\n")
	sb.WriteString("|------|-------------|--------|--------|\n")

	step := 1
	for _, env := range envs {
		details := fmt.Sprintf("promoted from %s", env.SourceName)
		if env.UnitCount > 0 {
			details = fmt.Sprintf("%d units changed", env.UnitCount)
		}

		if len(env.Regions) <= 1 {
			region := ""
			if len(env.Regions) == 1 {
				region = env.Regions[0]
			}
			sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s |\n", step, env.Name, region, details))
			step++
		} else {
			for i, region := range env.Regions {
				label := region
				if i == 0 {
					label = region + " (canary)"
				}
				sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s |\n", step, env.Name, label, details))
				step++
			}
		}
	}

	sb.WriteString("\nEach environment is deployed sequentially with manual approval gates between stages.\n")
	return sb.String()
}

func truncateOutput(output string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 10000
	}
	if len(output) <= maxLen {
		return output
	}
	return output[:maxLen] + "\n\n... (output truncated)"
}
