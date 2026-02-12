package runner

import (
	"regexp"
	"strconv"
	"strings"
)

// PlanSummary holds parsed resource change counts from a plan.
type PlanSummary struct {
	ToAdd     int
	ToChange  int
	ToDestroy int
	NoChanges bool
}

// HasChanges returns true if the plan includes any resource modifications.
func (s *PlanSummary) HasChanges() bool {
	return s.ToAdd > 0 || s.ToChange > 0 || s.ToDestroy > 0
}

// String returns a human-readable summary.
func (s *PlanSummary) String() string {
	if s.NoChanges {
		return "No changes"
	}
	return strconv.Itoa(s.ToAdd) + " to add, " +
		strconv.Itoa(s.ToChange) + " to change, " +
		strconv.Itoa(s.ToDestroy) + " to destroy"
}

// planSummaryRe matches lines like "Plan: 3 to add, 1 to change, 0 to destroy."
var planSummaryRe = regexp.MustCompile(`Plan:\s+(\d+)\s+to add,\s+(\d+)\s+to change,\s+(\d+)\s+to destroy`)

// noChangesRe matches lines like "No changes. Infrastructure is up-to-date."
var noChangesRe = regexp.MustCompile(`No changes\.|Your infrastructure matches the configuration`)

// ParsePlanOutput extracts a PlanSummary from terragrunt/terraform plan output.
func ParsePlanOutput(output string) *PlanSummary {
	summary := &PlanSummary{}

	for _, line := range strings.Split(output, "\n") {
		if matches := planSummaryRe.FindStringSubmatch(line); len(matches) == 4 {
			summary.ToAdd, _ = strconv.Atoi(matches[1])
			summary.ToChange, _ = strconv.Atoi(matches[2])
			summary.ToDestroy, _ = strconv.Atoi(matches[3])
			return summary
		}
		if noChangesRe.MatchString(line) {
			summary.NoChanges = true
			return summary
		}
	}

	return summary
}
