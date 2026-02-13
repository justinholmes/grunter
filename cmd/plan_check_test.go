package cmd

import (
	"testing"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/runner"
)

func intPtr(n int) *int { return &n }

func TestCheckDestroyThreshold_NilMaxDestroy(t *testing.T) {
	env := &config.Environment{Name: "dev", Path: "envs/dev"}
	summary := &runner.PlanSummary{ToDestroy: 100}

	if err := checkDestroyThreshold(env, summary, false); err != nil {
		t.Errorf("expected no error when MaxDestroy is nil, got: %v", err)
	}
}

func TestCheckDestroyThreshold_WithinLimit(t *testing.T) {
	env := &config.Environment{Name: "prod", Path: "envs/prod", MaxDestroy: intPtr(5)}
	summary := &runner.PlanSummary{ToDestroy: 3}

	if err := checkDestroyThreshold(env, summary, false); err != nil {
		t.Errorf("expected no error when destroys within limit, got: %v", err)
	}
}

func TestCheckDestroyThreshold_AtLimit(t *testing.T) {
	env := &config.Environment{Name: "prod", Path: "envs/prod", MaxDestroy: intPtr(5)}
	summary := &runner.PlanSummary{ToDestroy: 5}

	if err := checkDestroyThreshold(env, summary, false); err != nil {
		t.Errorf("expected no error when destroys equal limit, got: %v", err)
	}
}

func TestCheckDestroyThreshold_ExceedsLimit(t *testing.T) {
	env := &config.Environment{Name: "prod", Path: "envs/prod", MaxDestroy: intPtr(5)}
	summary := &runner.PlanSummary{ToDestroy: 6}

	err := checkDestroyThreshold(env, summary, false)
	if err == nil {
		t.Fatal("expected error when destroys exceed limit")
	}
}

func TestCheckDestroyThreshold_ZeroLimit_AnyDestroysBlocked(t *testing.T) {
	env := &config.Environment{Name: "staging", Path: "envs/staging", MaxDestroy: intPtr(0)}
	summary := &runner.PlanSummary{ToDestroy: 1}

	err := checkDestroyThreshold(env, summary, false)
	if err == nil {
		t.Fatal("expected error when max_destroy=0 and destroys > 0")
	}
}

func TestCheckDestroyThreshold_ZeroLimit_NoDestroys(t *testing.T) {
	env := &config.Environment{Name: "staging", Path: "envs/staging", MaxDestroy: intPtr(0)}
	summary := &runner.PlanSummary{ToDestroy: 0, ToAdd: 3, ToChange: 1}

	if err := checkDestroyThreshold(env, summary, false); err != nil {
		t.Errorf("expected no error when no destroys and max_destroy=0, got: %v", err)
	}
}

func TestCheckDestroyThreshold_AllowDestroyOverride(t *testing.T) {
	env := &config.Environment{Name: "prod", Path: "envs/prod", MaxDestroy: intPtr(0)}
	summary := &runner.PlanSummary{ToDestroy: 10}

	if err := checkDestroyThreshold(env, summary, true); err != nil {
		t.Errorf("expected no error with --allow-destroy, got: %v", err)
	}
}

func TestCheckDestroyThreshold_NoChanges(t *testing.T) {
	env := &config.Environment{Name: "prod", Path: "envs/prod", MaxDestroy: intPtr(0)}
	summary := &runner.PlanSummary{NoChanges: true}

	if err := checkDestroyThreshold(env, summary, false); err != nil {
		t.Errorf("expected no error with no changes, got: %v", err)
	}
}
