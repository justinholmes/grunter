package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/gitlab"
	"github.com/justinholmes/grunter/internal/plan"
	"github.com/spf13/cobra"
)

var deploymentSummaryCmd = &cobra.Command{
	Use:   "deployment-summary",
	Short: "Post a deployment summary comment to a GitLab MR",
	Long:  "Analyzes the execution plan to determine which environments are affected and posts a deployment summary showing the full promotion chain.",
	RunE:  runDeploymentSummary,
}

func init() {
	deploymentSummaryCmd.Flags().String("input", "execution-plan.json", "path to execution-plan.json")
	deploymentSummaryCmd.Flags().String("project-id", "", "GitLab project ID (default: CI_PROJECT_ID)")
	deploymentSummaryCmd.Flags().String("mr-iid", "", "MR IID (default: CI_MERGE_REQUEST_IID)")
	deploymentSummaryCmd.Flags().String("gitlab-url", "", "GitLab URL (default: CI_SERVER_URL or https://gitlab.com)")
	rootCmd.AddCommand(deploymentSummaryCmd)
}

func runDeploymentSummary(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Environments) == 0 {
		fmt.Fprintln(os.Stderr, "No environments configured, skipping deployment summary.")
		return nil
	}

	inputPath, _ := cmd.Flags().GetString("input")
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	var execPlan ExecutionPlan
	if err := json.Unmarshal(data, &execPlan); err != nil {
		return fmt.Errorf("parsing execution plan: %w", err)
	}

	// Count units per environment by matching unit paths to env paths
	envUnitCounts := make(map[string]int)
	for _, layer := range execPlan.Layers {
		for _, unit := range layer.Units {
			for _, env := range cfg.Environments {
				if strings.HasPrefix(unit.Path, env.Path+"/") {
					envUnitCounts[env.Name]++
					break
				}
			}
		}
	}

	// If no units match any configured environment, exit silently
	if len(envUnitCounts) == 0 {
		fmt.Fprintln(os.Stderr, "No environment files changed, skipping deployment summary.")
		return nil
	}

	// Build the full promotion chain — all environments in order
	deployEnvs := make([]plan.DeploymentEnv, len(cfg.Environments))
	for i, env := range cfg.Environments {
		de := plan.DeploymentEnv{
			Name:      env.Name,
			Regions:   env.Regions,
			UnitCount: envUnitCounts[env.Name],
		}
		if de.UnitCount == 0 && i > 0 {
			// Find the nearest upstream env that has changes
			for j := i - 1; j >= 0; j-- {
				de.SourceName = cfg.Environments[j].Name
				break
			}
		}
		deployEnvs[i] = de
	}

	formatted := plan.FormatDeploymentSummary(deployEnvs)

	// Post as MR comment
	projectID, _ := cmd.Flags().GetString("project-id")
	if projectID == "" {
		projectID = os.Getenv("CI_PROJECT_ID")
	}
	mrIID, _ := cmd.Flags().GetString("mr-iid")
	if mrIID == "" {
		mrIID = os.Getenv("CI_MERGE_REQUEST_IID")
	}
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
		return fmt.Errorf("GITLAB_TOKEN or CI_JOB_TOKEN must be set")
	}

	client := gitlab.NewClientWithTokenType(gitlabURL, token, tokenType)

	// In post-merge pipelines, CI_MERGE_REQUEST_IID is unavailable.
	// Fall back to looking up the MR from the commit SHA.
	if mrIID == "" {
		commitSHA := os.Getenv("CI_COMMIT_SHA")
		if commitSHA != "" {
			mrs, err := client.GetMRsForCommit(projectID, commitSHA)
			if err != nil {
				return fmt.Errorf("looking up MR for commit: %w", err)
			}
			if len(mrs) > 0 {
				mrIID = strconv.Itoa(mrs[0].IID)
			}
		}
	}

	if projectID == "" || mrIID == "" {
		return fmt.Errorf("project-id and mr-iid are required (set flags or CI_PROJECT_ID/CI_MERGE_REQUEST_IID)")
	}

	if err := client.PostOrUpdateComment(projectID, mrIID, "deployment-summary", formatted); err != nil {
		return fmt.Errorf("posting comment: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Deployment summary posted to MR")
	return nil
}
