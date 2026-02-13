package envs

import (
	"path/filepath"
	"testing"
)

func TestBuildPromotionPlan(t *testing.T) {
	dev, prod := setupDiffEnvs(t)

	plan, err := BuildPromotionPlan(dev, prod)
	if err != nil {
		t.Fatal(err)
	}

	if plan.SourceEnv != "dev" {
		t.Errorf("expected source=dev, got %s", plan.SourceEnv)
	}
	if plan.TargetEnv != "prod" {
		t.Errorf("expected target=prod, got %s", plan.TargetEnv)
	}

	// debug is only in dev → new unit
	if len(plan.NewUnits) != 1 {
		t.Fatalf("expected 1 new unit, got %d", len(plan.NewUnits))
	}
	if plan.NewUnits[0].LogicalName != filepath.Join("us-east-1", "debug") {
		t.Errorf("expected new unit=debug, got %s", plan.NewUnits[0].LogicalName)
	}

	// monitoring is only in prod → orphaned
	if len(plan.OrphanedUnits) != 1 {
		t.Fatalf("expected 1 orphaned unit, got %d", len(plan.OrphanedUnits))
	}
	if plan.OrphanedUnits[0].LogicalName != filepath.Join("us-east-1", "monitoring") {
		t.Errorf("expected orphaned unit=monitoring, got %s", plan.OrphanedUnits[0].LogicalName)
	}

	// rds differs → changed (vpc is identical, should not be listed)
	if len(plan.ChangedUnits) != 1 {
		t.Fatalf("expected 1 changed unit, got %d", len(plan.ChangedUnits))
	}
	if plan.ChangedUnits[0].LogicalName != filepath.Join("us-east-1", "rds") {
		t.Errorf("expected changed unit=rds, got %s", plan.ChangedUnits[0].LogicalName)
	}
}
