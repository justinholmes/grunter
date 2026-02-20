package lint

import (
	"bytes"
	"fmt"
	"os/exec"
)

// CheckKind identifies the type of lint check.
type CheckKind string

const (
	KindHCLFmt   CheckKind = "hclfmt"
	KindFmt      CheckKind = "fmt"
	KindValidate CheckKind = "validate"
	KindTFLint   CheckKind = "tflint"
)

// Result holds the outcome of a single lint check.
type Result struct {
	Kind    CheckKind
	Path    string // unit or module dir
	Passed  bool
	Skipped bool
	Output  string // stderr/stdout from the tool
}

// Linter runs code quality checks against terragrunt units and terraform modules.
type Linter struct {
	tfBinary string
	tgBinary string
}

// New creates a Linter with the given binary names.
func New(tfBinary, tgBinary string) *Linter {
	return &Linter{
		tfBinary: tfBinary,
		tgBinary: tgBinary,
	}
}

// CheckHCLFmt runs `terragrunt hclfmt --terragrunt-check` in the unit dir.
func (l *Linter) CheckHCLFmt(unitDir string) Result {
	cmd := exec.Command(l.tgBinary, "hcl", "fmt", "--check")
	cmd.Dir = unitDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := combinedOutput(stdout.String(), stderr.String())

	return Result{
		Kind:   KindHCLFmt,
		Path:   unitDir,
		Passed: err == nil,
		Output: output,
	}
}

// CheckTFFmt runs `tofu fmt -check` in the module dir.
func (l *Linter) CheckTFFmt(moduleDir string) Result {
	cmd := exec.Command(l.tfBinary, "fmt", "-check")
	cmd.Dir = moduleDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := combinedOutput(stdout.String(), stderr.String())

	return Result{
		Kind:   KindFmt,
		Path:   moduleDir,
		Passed: err == nil,
		Output: output,
	}
}

// Validate runs `terragrunt validate` in the unit dir.
func (l *Linter) Validate(unitDir string) Result {
	cmd := exec.Command(l.tgBinary, "validate")
	cmd.Dir = unitDir
	cmd.Env = append(cmd.Environ(),
		"TF_CLI_ARGS_validate=-no-color",
	)
	if l.tfBinary != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERRAGRUNT_TFPATH=%s", l.tfBinary))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := combinedOutput(stdout.String(), stderr.String())

	return Result{
		Kind:   KindValidate,
		Path:   unitDir,
		Passed: err == nil,
		Output: output,
	}
}

// CheckTFLint runs `tflint` in the module dir. If tflint is not installed, returns a skip result.
func (l *Linter) CheckTFLint(moduleDir string) Result {
	if _, err := exec.LookPath("tflint"); err != nil {
		return Result{
			Kind:    KindTFLint,
			Path:    moduleDir,
			Skipped: true,
			Passed:  true,
			Output:  "tflint not found in PATH, skipping",
		}
	}

	cmd := exec.Command("tflint")
	cmd.Dir = moduleDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := combinedOutput(stdout.String(), stderr.String())

	return Result{
		Kind:   KindTFLint,
		Path:   moduleDir,
		Passed: err == nil,
		Output: output,
	}
}

func combinedOutput(stdout, stderr string) string {
	if stderr == "" {
		return stdout
	}
	if stdout == "" {
		return stderr
	}
	return stdout + "\n" + stderr
}
