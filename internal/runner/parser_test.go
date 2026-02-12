package runner

import "testing"

func TestParsePlanOutput_WithChanges(t *testing.T) {
	output := `
Terraform used the selected providers to generate the following execution plan.

  # aws_instance.web will be created
  + resource "aws_instance" "web" {
      + ami           = "ami-12345"
      + instance_type = "t3.micro"
    }

  # aws_security_group.web will be created
  + resource "aws_security_group" "web" {
      + name = "web-sg"
    }

  # aws_db_instance.main will be updated in-place
  ~ resource "aws_db_instance" "main" {
      ~ instance_class = "db.t3.micro" -> "db.t3.small"
    }

Plan: 2 to add, 1 to change, 0 to destroy.
`

	summary := ParsePlanOutput(output)

	if summary.ToAdd != 2 {
		t.Errorf("expected 2 to add, got %d", summary.ToAdd)
	}
	if summary.ToChange != 1 {
		t.Errorf("expected 1 to change, got %d", summary.ToChange)
	}
	if summary.ToDestroy != 0 {
		t.Errorf("expected 0 to destroy, got %d", summary.ToDestroy)
	}
	if summary.NoChanges {
		t.Error("expected NoChanges to be false")
	}
	if !summary.HasChanges() {
		t.Error("expected HasChanges to be true")
	}
}

func TestParsePlanOutput_NoChanges(t *testing.T) {
	output := `No changes. Infrastructure is up-to-date.`

	summary := ParsePlanOutput(output)

	if !summary.NoChanges {
		t.Error("expected NoChanges to be true")
	}
	if summary.HasChanges() {
		t.Error("expected HasChanges to be false")
	}
}

func TestParsePlanOutput_MatchesConfiguration(t *testing.T) {
	output := `No changes. Your infrastructure matches the configuration.`

	summary := ParsePlanOutput(output)

	if !summary.NoChanges {
		t.Error("expected NoChanges to be true")
	}
}

func TestParsePlanOutput_DestroyOnly(t *testing.T) {
	output := `Plan: 0 to add, 0 to change, 3 to destroy.`

	summary := ParsePlanOutput(output)

	if summary.ToAdd != 0 {
		t.Errorf("expected 0 to add, got %d", summary.ToAdd)
	}
	if summary.ToDestroy != 3 {
		t.Errorf("expected 3 to destroy, got %d", summary.ToDestroy)
	}
	if !summary.HasChanges() {
		t.Error("expected HasChanges to be true")
	}
}

func TestPlanSummary_String(t *testing.T) {
	tests := []struct {
		summary  PlanSummary
		expected string
	}{
		{PlanSummary{NoChanges: true}, "No changes"},
		{PlanSummary{ToAdd: 1, ToChange: 2, ToDestroy: 3}, "1 to add, 2 to change, 3 to destroy"},
	}

	for _, tt := range tests {
		got := tt.summary.String()
		if got != tt.expected {
			t.Errorf("String() = %q, want %q", got, tt.expected)
		}
	}
}
