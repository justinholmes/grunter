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
	rfcCheckCmd.Flags().String("change-number", "", "change number / identifier (default: from env var configured in config)")
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
	case config.RFCSourceCustom:
		checker, err = buildCustomChecker(cmd, env)
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
		var windowResult *rfc.WindowResult

		if env.DeploymentWindow.Source == config.DeploymentWindowCustom {
			cwc, err := buildCustomWindowChecker(env)
			if err != nil {
				return fmt.Errorf("building custom window checker: %w", err)
			}
			windowResult, err = cwc.CheckWindow()
			if err != nil {
				return fmt.Errorf("deployment window check failed: %w", err)
			}
		} else {
			windowResult, err = rfc.CheckDeploymentWindow(env.DeploymentWindow, time.Now())
			if err != nil {
				return fmt.Errorf("deployment window check failed: %w", err)
			}
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

func buildCustomChecker(cmd *cobra.Command, env *config.Environment) (*rfc.CustomChecker, error) {
	changeID, _ := cmd.Flags().GetString("change-number")
	if changeID == "" && env.RFC.ChangeNumberVar != "" {
		changeID = os.Getenv(env.RFC.ChangeNumberVar)
	}

	token, username, headerName, err := resolveAuth(env.RFC.AuthType, env.RFC.AuthTokenVar, env.RFC.AuthUsernameVar, env.RFC.AuthHeaderName)
	if err != nil {
		return nil, err
	}

	return &rfc.CustomChecker{
		HTTPClient:      &http.Client{Timeout: 30 * time.Second},
		URL:             env.RFC.URL,
		Method:          env.RFC.Method,
		AuthType:        env.RFC.AuthType,
		AuthToken:       token,
		AuthUsername:     username,
		AuthHeaderName:  headerName,
		ResponseType:    env.RFC.ResponseType,
		ResponsePath:    env.RFC.ResponsePath,
		ApprovedField:   env.RFC.ApprovedField,
		ApprovedValue:   env.RFC.ApprovedValue,
		EmergencyField:  env.RFC.EmergencyField,
		EmergencyValue:  env.RFC.EmergencyValue,
		IdentifierField: env.RFC.IdentifierField,
		ChangeID:        changeID,
	}, nil
}

func buildCustomWindowChecker(env *config.Environment) (*rfc.CustomWindowChecker, error) {
	w := env.DeploymentWindow
	token, username, headerName, err := resolveAuth(w.AuthType, w.AuthTokenVar, w.AuthUsernameVar, w.AuthHeaderName)
	if err != nil {
		return nil, err
	}

	return &rfc.CustomWindowChecker{
		HTTPClient:      &http.Client{Timeout: 30 * time.Second},
		URL:             w.URL,
		Method:          w.Method,
		AuthType:        w.AuthType,
		AuthToken:       token,
		AuthUsername:     username,
		AuthHeaderName:  headerName,
		ResponsePath:    w.ResponsePath,
		AllowedField:    w.AllowedField,
		MessageField:    w.MessageField,
		NextWindowField: w.NextWindowField,
	}, nil
}

// resolveAuth reads auth credentials from environment variables based on auth type.
func resolveAuth(authType, tokenVar, usernameVar, headerName string) (token, username, header string, err error) {
	if authType == "" {
		return "", "", "", nil
	}

	header = headerName

	if tokenVar != "" {
		token = os.Getenv(tokenVar)
		if token == "" {
			return "", "", "", fmt.Errorf("environment variable %s is not set (required for %s auth)", tokenVar, authType)
		}
	}

	if authType == "basic" && usernameVar != "" {
		username = os.Getenv(usernameVar)
		if username == "" {
			return "", "", "", fmt.Errorf("environment variable %s is not set (required for basic auth)", usernameVar)
		}
	}

	return token, username, header, nil
}
