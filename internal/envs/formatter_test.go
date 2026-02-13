package envs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatDiffMarkdown(t *testing.T) {
	result := &EnvDiffResult{
		SourceEnv: "dev",
		TargetEnv: "prod",
		Structural: StructuralDiff{
			OnlyInSource: []UnitInventory{
				{LogicalName: "us-east-1/debug"},
			},
			OnlyInTarget: []UnitInventory{
				{LogicalName: "us-east-1/monitoring"},
			},
		},
		ContentDiffs: []ContentDiff{
			{LogicalName: "us-east-1/vpc", Identical: true},
			{LogicalName: "us-east-1/rds", Identical: false, Unified: "--- a\n+++ b\n@@ -1 +1 @@\n-dev\n+prod\n"},
		},
	}

	md := FormatDiffMarkdown(result)

	if !strings.Contains(md, "Environment Diff: dev → prod") {
		t.Error("expected header with env names")
	}
	if !strings.Contains(md, "Only in **dev** | 1") {
		t.Error("expected only-in-source count")
	}
	if !strings.Contains(md, "Only in **prod** | 1") {
		t.Error("expected only-in-target count")
	}
	if !strings.Contains(md, "Shared — identical | 1") {
		t.Error("expected identical count")
	}
	if !strings.Contains(md, "Shared — different | 1") {
		t.Error("expected different count")
	}
	if !strings.Contains(md, "`us-east-1/debug`") {
		t.Error("expected source-only unit listed")
	}
	if !strings.Contains(md, "`us-east-1/monitoring`") {
		t.Error("expected target-only unit listed")
	}
	if !strings.Contains(md, "<details open>") {
		t.Error("expected collapsible content diff")
	}
	if !strings.Contains(md, "```diff") {
		t.Error("expected diff code block")
	}
}

func TestFormatDiffMarkdown_NoDifferences(t *testing.T) {
	result := &EnvDiffResult{
		SourceEnv: "dev",
		TargetEnv: "staging",
		ContentDiffs: []ContentDiff{
			{LogicalName: "us-east-1/vpc", Identical: true},
		},
	}

	md := FormatDiffMarkdown(result)

	if strings.Contains(md, "Content differences") {
		t.Error("should not show content differences section when all identical")
	}
}

func TestFormatDiffJSON(t *testing.T) {
	result := &EnvDiffResult{
		SourceEnv: "dev",
		TargetEnv: "prod",
		Structural: StructuralDiff{
			OnlyInSource: []UnitInventory{
				{LogicalName: "us-east-1/debug", FullPath: "envs/dev/us-east-1/debug", Region: "us-east-1"},
			},
		},
	}

	jsonStr, err := FormatDiffJSON(result)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["SourceEnv"] != "dev" {
		t.Errorf("expected SourceEnv=dev, got %v", parsed["SourceEnv"])
	}
}
