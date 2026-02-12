package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Result holds the output of a terragrunt command execution.
type Result struct {
	Output  string
	Stderr  string
	Success bool
}

// CombinedOutput returns stdout and stderr concatenated.
// Terragrunt writes its own logs to stderr while terraform plan output
// goes to stdout, so both streams are needed for full context.
func (r *Result) CombinedOutput() string {
	if r.Stderr == "" {
		return r.Output
	}
	if r.Output == "" {
		return r.Stderr
	}
	return r.Output + "\n" + r.Stderr
}

// Runner executes terragrunt commands.
type Runner struct {
	tfBinary string
	tgBinary string
}

// New creates a Runner with the given binary paths.
func New(tfBinary, tgBinary string) *Runner {
	return &Runner{
		tfBinary: tfBinary,
		tgBinary: tgBinary,
	}
}

// Run executes terragrunt plan or apply in the given unit directory.
// A zero timeout means no timeout.
func (r *Runner) Run(unitPath, action string, timeout time.Duration) (*Result, error) {
	args := []string{action}

	if action == "plan" {
		args = append(args, "-detailed-exitcode")
	}
	if action == "apply" {
		args = append(args, "-auto-approve")
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, r.tgBinary, args...)
	cmd.Dir = unitPath
	cmd.Env = append(cmd.Environ(),
		fmt.Sprintf("TF_CLI_ARGS_plan=-no-color"),
		fmt.Sprintf("TF_CLI_ARGS_apply=-no-color"),
	)

	if r.tfBinary != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERRAGRUNT_TFPATH=%s", r.tfBinary))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &Result{
		Output: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 2 from plan with -detailed-exitcode means changes detected (not an error)
			if action == "plan" && exitErr.ExitCode() == 2 {
				result.Success = true
				return result, nil
			}
			result.Success = false
			return result, nil
		}
		return nil, fmt.Errorf("executing %s %s in %s: %w", r.tgBinary, action, unitPath, err)
	}

	result.Success = true
	return result, nil
}
