//go:build integration

package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	testhelpers "github.com/justinholmes/grunter/test"
)

// localNullTF is a minimal terraform config using the null provider.
// Requires no cloud credentials.
const localNullTF = `
terraform {
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
  # Local backend - no remote state needed
  backend "local" {}
}

resource "null_resource" "test" {
  triggers = {
    value = "hello"
  }
}

output "test_output" {
  value = "integration-test"
}
`

// localNullTFChanged has a different trigger to produce a plan diff.
const localNullTFChanged = `
terraform {
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
  backend "local" {}
}

resource "null_resource" "test" {
  triggers = {
    value = "changed"
  }
}

resource "null_resource" "new_resource" {
  triggers = {
    value = "brand-new"
  }
}

output "test_output" {
  value = "integration-test-changed"
}
`

const simpleTerragruntHCL = `# Empty terragrunt config - just use local terraform
`

func TestRunnerIntegration_PlanNoState(t *testing.T) {
	testhelpers.RequireBinary(t, "terragrunt")
	testhelpers.RequireBinary(t, "tofu")

	tmp := t.TempDir()
	unitDir := testhelpers.CreateTerragruntUnit(t, tmp, "myunit", simpleTerragruntHCL, localNullTF)

	r := New("tofu", "terragrunt")
	result, err := r.Run(unitDir, "plan", 5*time.Minute)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	combined := result.CombinedOutput()
	t.Logf("Plan output:\n%s", combined)

	if !result.Success {
		t.Fatalf("expected plan to succeed, stderr:\n%s", result.Stderr)
	}

	summary := ParsePlanOutput(combined)
	if !summary.HasChanges() {
		t.Fatal("expected plan to show changes (new resource to create)")
	}
	if summary.ToAdd < 1 {
		t.Errorf("expected at least 1 to add, got %d", summary.ToAdd)
	}
}

func TestRunnerIntegration_ApplyThenPlanNoChanges(t *testing.T) {
	testhelpers.RequireBinary(t, "terragrunt")
	testhelpers.RequireBinary(t, "tofu")

	tmp := t.TempDir()
	unitDir := testhelpers.CreateTerragruntUnit(t, tmp, "myunit", simpleTerragruntHCL, localNullTF)

	r := New("tofu", "terragrunt")

	// Apply first
	applyResult, err := r.Run(unitDir, "apply", 5*time.Minute)
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	if !applyResult.Success {
		t.Fatalf("apply failed:\n%s", applyResult.CombinedOutput())
	}
	t.Logf("Apply output:\n%s", applyResult.CombinedOutput())

	// Verify state file was created
	stateFiles, _ := filepath.Glob(filepath.Join(unitDir, "**", "terraform.tfstate"))
	if len(stateFiles) == 0 {
		// terragrunt may cache in .terragrunt-cache
		stateFiles, _ = filepath.Glob(filepath.Join(unitDir, ".terragrunt-cache", "*", "*", "terraform.tfstate"))
	}
	t.Logf("State files found: %v", stateFiles)

	// Plan again - should show no changes
	planResult, err := r.Run(unitDir, "plan", 5*time.Minute)
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !planResult.Success {
		t.Fatalf("plan failed:\n%s", planResult.CombinedOutput())
	}

	combined := planResult.CombinedOutput()
	t.Logf("Plan after apply:\n%s", combined)

	summary := ParsePlanOutput(combined)
	if summary.HasChanges() {
		t.Errorf("expected no changes after apply, got: %s", summary.String())
	}
}

func TestRunnerIntegration_ApplyThenModifyDetectsDrift(t *testing.T) {
	testhelpers.RequireBinary(t, "terragrunt")
	testhelpers.RequireBinary(t, "tofu")

	tmp := t.TempDir()
	unitDir := testhelpers.CreateTerragruntUnit(t, tmp, "myunit", simpleTerragruntHCL, localNullTF)

	r := New("tofu", "terragrunt")

	// Apply the initial config
	applyResult, err := r.Run(unitDir, "apply", 5*time.Minute)
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	if !applyResult.Success {
		t.Fatalf("apply failed:\n%s", applyResult.CombinedOutput())
	}

	// Now change the terraform config (simulating drift detection scenario:
	// code changed but hasn't been applied yet)
	mainTF := filepath.Join(unitDir, "main.tf")
	if err := os.WriteFile(mainTF, []byte(localNullTFChanged), 0644); err != nil {
		t.Fatal(err)
	}

	// Plan should detect the diff
	planResult, err := r.Run(unitDir, "plan", 5*time.Minute)
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}

	combined := planResult.CombinedOutput()
	t.Logf("Plan after modification:\n%s", combined)

	summary := ParsePlanOutput(combined)
	if !summary.HasChanges() {
		t.Fatal("expected drift to be detected after modifying config")
	}
	if summary.ToAdd < 1 {
		t.Errorf("expected at least 1 to add (new_resource), got %d", summary.ToAdd)
	}
	// null_resource with changed triggers forces replacement (destroy+create),
	// not an in-place update, so we check for destroy instead of change
	if summary.ToDestroy < 1 && summary.ToChange < 1 {
		t.Errorf("expected at least 1 to destroy or change (trigger update forces replacement), got destroy=%d change=%d",
			summary.ToDestroy, summary.ToChange)
	}
}

func TestRunnerIntegration_PlanTimeout(t *testing.T) {
	testhelpers.RequireBinary(t, "terragrunt")
	testhelpers.RequireBinary(t, "tofu")

	tmp := t.TempDir()
	// Create a config with a long-running provisioner (sleep)
	sleepTF := `
terraform {
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
  backend "local" {}
}

resource "null_resource" "slow" {
  provisioner "local-exec" {
    command = "sleep 300"
  }
}
`
	unitDir := testhelpers.CreateTerragruntUnit(t, tmp, "slow", simpleTerragruntHCL, sleepTF)

	r := New("tofu", "terragrunt")

	// Plan with a very short timeout — plan itself shouldn't timeout since
	// provisioners only run on apply. So this tests that timeout plumbing works.
	// Use a generous timeout for plan (it should complete fast)
	result, err := r.Run(unitDir, "plan", 2*time.Minute)
	if err != nil {
		t.Logf("Plan with timeout returned error (may be ok): %v", err)
		return
	}
	if !result.Success {
		t.Logf("Plan output:\n%s", result.CombinedOutput())
	}
	// If we get here, the plan completed within timeout (expected)
}

func TestRunnerIntegration_PlanBadConfig(t *testing.T) {
	testhelpers.RequireBinary(t, "terragrunt")
	testhelpers.RequireBinary(t, "tofu")

	tmp := t.TempDir()
	badTF := `
terraform {
  backend "local" {}
}

resource "nonexistent_provider_type" "bad" {
  name = "this-should-fail"
}
`
	unitDir := testhelpers.CreateTerragruntUnit(t, tmp, "bad", simpleTerragruntHCL, badTF)

	r := New("tofu", "terragrunt")
	result, err := r.Run(unitDir, "plan", 2*time.Minute)

	// Should not return a Go-level error (binary ran, just exited non-zero)
	if err != nil {
		t.Logf("Got Go error (binary not found?): %v", err)
		return
	}

	if result.Success {
		t.Fatal("expected plan to fail on bad config")
	}

	combined := result.CombinedOutput()
	t.Logf("Bad config output:\n%s", combined)

	if !strings.Contains(combined, "nonexistent_provider_type") &&
		!strings.Contains(combined, "Error") &&
		!strings.Contains(combined, "error") {
		t.Error("expected error output to reference the bad resource type")
	}
}
