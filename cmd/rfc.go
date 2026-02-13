package cmd

import (
	"fmt"
	"net/http"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/gitlab"
	"github.com/justinholmes/grunter/internal/rfc"
	"github.com/spf13/cobra"
)

var rfcCheckCmd = &cobra.Command{
	Use:   "rfc-check",
	Short: "Verify RFC approval and deployment window",
	Long:  "Checks that a deployment has an approved RFC/change request and falls within the configured deployment window. Used as a gate before environment deployments.",
	RunE:  runRFCCheck,
}

func init() {
	rfcCheckCmd.Flags().String("env", "", "environment name to check")
	rfcCheckCmd.Flags().String("project-id", "", "GitLab project ID (default: CI_PROJECT_ID)")
	rfcCheckCmd.Flags().String("mr-iid", "", "MR IID (default: CI_MERGE_REQUEST_IID)")
	rfcCheckCmd.Flags().String("gitlab-url", "", "GitLab URL (default: CI_SERVER_URL or https://gitlab.com)")
	rfcCheckCmd.Flags().String("change-number", "", "ServiceNow change number (default: from env var configured in config)")
	rfcCheckCmd.Flags().Bool("skip-window", false, "skip deployment window check")
	rootCmd.AddCommand(rfcCheckCmd)
}

func runRFCCheck(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	envName, _ := cmd.Flags().GetString("env")
	if envName == "" {
		return fmt.Errorf("--env is required")
	}

	env := cfg.GetEnvironment(envName)
	if env == nil {
		return fmt.Errorf("environment %q not found in config", envName)
	}

	// No RFC config means no checks needed
	if !env.RequiresRFCCheck() {
		fmt.Fprintf(os.Stderr, "No RFC config for environment %q, skipping checks\n", envName)
		return nil
	}

	// Build the appropriate checker
	var checker rfc.Checker

	switch env.RFC.Source {
	case config.RFCSourceGitLab:
		checker, err = buildGitLabChecker(cmd, env)
		if err != nil {
			return err
		}
	case config.RFCSourceServiceNow:
		checker, err = buildServiceNowChecker(cmd, env)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported RFC source %q", env.RFC.Source)
	}

	// Run RFC check
	result, err := checker.CheckRFC()
	if err != nil {
		return fmt.Errorf("RFC check failed: %w", err)
	}

	if !result.Approved {
		fmt.Fprintf(os.Stderr, "RFC check FAILED: %s\n", result.Message)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "RFC check passed: %s\n", result.Message)

	if result.Emergency {
		fmt.Fprintf(os.Stderr, "Emergency RFC detected — deployment window check bypassed\n")
		return nil
	}

	// Deployment window check
	skipWindow, _ := cmd.Flags().GetBool("skip-window")
	if !skipWindow && env.HasDeploymentWindow() {
		windowResult, err := rfc.CheckDeploymentWindow(env.DeploymentWindow, time.Now())
		if err != nil {
			return fmt.Errorf("deployment window check failed: %w", err)
		}

		if !windowResult.Allowed {
			msg := fmt.Sprintf("Deployment window check FAILED: %s", windowResult.Message)
			if windowResult.NextWindowStart != nil {
				msg += fmt.Sprintf(" (next window: %s)", windowResult.NextWindowStart.Format(time.RFC3339))
			}
			fmt.Fprintln(os.Stderr, msg)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "Deployment window check passed: %s\n", windowResult.Message)
	}

	return nil
}

func buildGitLabChecker(cmd *cobra.Command, env *config.Environment) (*rfc.GitLabChecker, error) {
	projectID, _ := cmd.Flags().GetString("project-id")
	if projectID == "" {
		projectID = os.Getenv("CI_PROJECT_ID")
	}
	if projectID == "" {
		return nil, fmt.Errorf("project-id is required (set --project-id or CI_PROJECT_ID)")
	}

	mrIID, _ := cmd.Flags().GetString("mr-iid")
	if mrIID == "" {
		mrIID = os.Getenv("CI_MERGE_REQUEST_IID")
	}

	commitSHA := os.Getenv("CI_COMMIT_SHA")

	gitlabURL, _ := cmd.Flags().GetString("gitlab-url")
	if gitlabURL == "" {
		gitlabURL = os.Getenv("CI_SERVER_URL")
	}
	if gitlabURL == "" {
		gitlabURL = "https://gitlab.com"
	}

	token := os.Getenv("GITLAB_TOKEN")
	tokenType := gitlab.PrivateToken
	if token == "" {
		token = os.Getenv("CI_JOB_TOKEN")
		tokenType = gitlab.JobToken
	}
	if token == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN or CI_JOB_TOKEN must be set")
	}

	client := gitlab.NewClientWithTokenType(gitlabURL, token, tokenType)

	return &rfc.GitLabChecker{
		Client:    client,
		ProjectID: projectID,
		MRIID:     mrIID,
		CommitSHA: commitSHA,
		Config:    env.RFC,
	}, nil
}

func buildServiceNowChecker(cmd *cobra.Command, env *config.Environment) (*rfc.ServiceNowChecker, error) {
	changeNumber, _ := cmd.Flags().GetString("change-number")
	if changeNumber == "" && env.RFC.ChangeNumberVar != "" {
		changeNumber = os.Getenv(env.RFC.ChangeNumberVar)
	}

	username := os.Getenv("SERVICENOW_USERNAME")
	password := os.Getenv("SERVICENOW_PASSWORD")
	if username == "" || password == "" {
		return nil, fmt.Errorf("SERVICENOW_USERNAME and SERVICENOW_PASSWORD must be set")
	}

	return &rfc.ServiceNowChecker{
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		InstanceURL:  env.RFC.InstanceURL,
		Username:     username,
		Password:     password,
		ChangeNumber: changeNumber,
		Config:       env.RFC,
	}, nil
}
