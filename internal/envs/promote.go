package envs

import "github.com/justinholmes/grunter/internal/config"

// BuildPromotionPlan wraps Diff and categorizes the results into new, changed,
// and orphaned units for promoting from source to target environment.
func BuildPromotionPlan(source, target config.Environment) (*PromotionPlan, error) {
	diffResult, err := Diff(source, target)
	if err != nil {
		return nil, err
	}

	plan := &PromotionPlan{
		SourceEnv: source.Name,
		TargetEnv: target.Name,
	}

	// Units in source but not target → new units to create
	plan.NewUnits = diffResult.Structural.OnlyInSource

	// Units in target but not source → orphaned
	plan.OrphanedUnits = diffResult.Structural.OnlyInTarget

	// Shared units with content differences → changed
	for _, cd := range diffResult.ContentDiffs {
		if !cd.Identical {
			plan.ChangedUnits = append(plan.ChangedUnits, cd)
		}
	}

	return plan, nil
}
