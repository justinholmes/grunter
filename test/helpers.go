package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// RequireBinary skips the test if the given binary is not available on PATH.
func RequireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("skipping: %s not found on PATH", name)
	}
}

// RequireContainerRuntime skips the test if neither podman nor docker is available.
// Returns the path to whichever runtime was found (prefers podman).
func RequireContainerRuntime(t *testing.T) string {
	t.Helper()
	// Try podman first: wrapper script, then .exe, then PATH
	for _, candidate := range []string{
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "podman-host"),
		"/mnt/c/Program Files/RedHat/Podman/podman.exe",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if path, err := exec.LookPath("podman"); err == nil {
		return path
	}
	// Fall back to docker
	if path, err := exec.LookPath("docker"); err == nil {
		return path
	}
	t.Skip("skipping: neither podman nor docker available")
	return ""
}

// RequirePodman is an alias for RequireContainerRuntime for backward compatibility.
func RequirePodman(t *testing.T) string {
	return RequireContainerRuntime(t)
}

// CreateTerragruntUnit creates a directory with a terragrunt.hcl and optional
// terraform files for integration testing. Returns the unit directory path.
func CreateTerragruntUnit(t *testing.T, parentDir, name, hclContent, tfContent string) string {
	t.Helper()
	dir := filepath.Join(parentDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte(hclContent), 0644); err != nil {
		t.Fatal(err)
	}
	if tfContent != "" {
		if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tfContent), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// InitGitRepo creates a git repo in dir with an initial commit.
func InitGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s\n%s", args, err, out)
		}
	}
}

// InitGitRepoWithMain creates a git repo with initial branch named "main".
func InitGitRepoWithMain(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s\n%s", args, err, out)
		}
	}
}

// GitCommitAll stages all changes and commits.
func GitCommitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	cmds := [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", msg},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s\n%s", args, err, out)
		}
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1]) // trim newline
}

// GitGetHEAD returns the current HEAD SHA.
func GitGetHEAD(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1])
}
