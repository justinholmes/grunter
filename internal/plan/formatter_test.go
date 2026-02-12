package plan

import (
	"strings"
	"testing"

	"github.com/justinholmes/grunter/internal/runner"
)

func TestFormatComment_PlanWithChanges(t *testing.T) {
	output := `Plan: 2 to add, 1 to change, 0 to destroy.`

	comment := FormatComment("envs/dev/vpc", "plan", output)

	if !strings.Contains(comment, "envs/dev/vpc") {
		t.Error("expected comment to contain unit path")
	}
	if !strings.Contains(comment, "plan") {
		t.Error("expected comment to contain action")
	}
	if !strings.Contains(comment, "2 to add") {
		t.Error("expected comment to contain summary")
	}
	if !strings.Contains(comment, "<details>") {
		t.Error("expected comment to contain collapsible section")
	}
}

func TestFormatComment_NoChanges(t *testing.T) {
	output := `No changes. Infrastructure is up-to-date.`

	comment := FormatComment("envs/dev/vpc", "plan", output)

	if !strings.Contains(comment, "No changes") {
		t.Error("expected comment to contain 'No changes'")
	}
}

func TestFormatMultiUnitComment(t *testing.T) {
	results := []UnitResult{
		{
			Path:    "envs/dev/vpc",
			Status:  StatusChanged,
			Output:  "Plan: 1 to add, 0 to change, 0 to destroy.",
			Summary: &runner.PlanSummary{ToAdd: 1},
		},
		{
			Path:    "envs/dev/rds",
			Status:  StatusNoChanges,
			Output:  "No changes.",
			Summary: &runner.PlanSummary{NoChanges: true},
		},
	}

	comment := FormatMultiUnitComment(results)

	if !strings.Contains(comment, "Grunter Plan Results") {
		t.Error("expected comment to contain header")
	}
	if !strings.Contains(comment, "envs/dev/vpc") {
		t.Error("expected comment to contain vpc unit")
	}
	if !strings.Contains(comment, "envs/dev/rds") {
		t.Error("expected comment to contain rds unit")
	}
	// Should have a summary table
	if !strings.Contains(comment, "| Status | Unit | Resources |") {
		t.Error("expected comment to contain summary table")
	}
}

func TestFormatDriftReport(t *testing.T) {
	results := []UnitResult{
		{
			Path:    "envs/dev/vpc",
			Status:  StatusDrift,
			Output:  "Plan: 1 to add, 0 to change, 0 to destroy.",
			Summary: &runner.PlanSummary{ToAdd: 1},
		},
	}

	report := FormatDriftReport(results)

	if !strings.Contains(report, "Drift Detection Report") {
		t.Error("expected report to contain header")
	}
	if !strings.Contains(report, "**1** unit(s)") {
		t.Error("expected report to contain unit count")
	}
}

func TestTruncateOutput(t *testing.T) {
	short := "hello"
	if truncateOutput(short, 100) != short {
		t.Error("short strings should not be truncated")
	}

	long := strings.Repeat("x", 200)
	truncated := truncateOutput(long, 100)
	if len(truncated) >= 200 {
		t.Error("expected truncation")
	}
	if !strings.Contains(truncated, "truncated") {
		t.Error("expected truncation marker")
	}
}
