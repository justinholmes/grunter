package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/justinholmes/grunter/internal/gitlab"
	"github.com/justinholmes/grunter/internal/plan"
	"github.com/spf13/cobra"
)

var commentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Post plan/apply results to a GitLab MR",
	Long:  "Reads plan or apply output and posts it as a formatted markdown comment on the GitLab Merge Request.",
	RunE:  runComment,
}

func init() {
	commentCmd.Flags().String("input", "", "read output from file (default: stdin)")
	commentCmd.Flags().String("unit", "", "unit path for the comment header")
	commentCmd.Flags().String("action", "plan", "action type: plan or apply")
	commentCmd.Flags().String("project-id", "", "GitLab project ID (default: CI_PROJECT_ID)")
	commentCmd.Flags().String("mr-iid", "", "MR IID (default: CI_MERGE_REQUEST_IID)")
	commentCmd.Flags().String("gitlab-url", "", "GitLab URL (default: CI_SERVER_URL or https://gitlab.com)")
	commentCmd.Flags().String("env", "", "include environment name in comment header and marker")
	commentCmd.Flags().String("commit-sha", "", "commit SHA for MR lookup fallback (default: CI_COMMIT_SHA)")
	commentCmd.Flags().Bool("raw", false, "post content as-is without formatting (for pre-formatted markdown)")
	rootCmd.AddCommand(commentCmd)
}

func runComment(cmd *cobra.Command, args []string) error {
	inputPath, _ := cmd.Flags().GetString("input")
	unitPath, _ := cmd.Flags().GetString("unit")
	action, _ := cmd.Flags().GetString("action")

	var content []byte
	var err error
	if inputPath != "" {
		content, err = os.ReadFile(inputPath)
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
	} else {
		content, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	}

	raw, _ := cmd.Flags().GetBool("raw")
	var formatted string
	if raw {
		formatted = string(content)
	} else {
		formatted = plan.FormatComment(unitPath, action, string(content))
	}

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
		commitSHA, _ := cmd.Flags().GetString("commit-sha")
		if commitSHA == "" {
			commitSHA = os.Getenv("CI_COMMIT_SHA")
		}
		if commitSHA != "" {
			mrs, err := client.GetMRsForCommit(projectID, commitSHA)
			if err != nil {
				return fmt.Errorf("looking up MR for commit %s: %w", commitSHA, err)
			}
			if len(mrs) > 0 {
				mrIID = strconv.Itoa(mrs[0].IID)
			}
		}
	}

	if projectID == "" || mrIID == "" {
		return fmt.Errorf("project-id and mr-iid are required (set flags or CI_PROJECT_ID/CI_MERGE_REQUEST_IID)")
	}

	// When --env is set, scope the comment marker to include the environment
	envName, _ := cmd.Flags().GetString("env")
	marker := unitPath
	if envName != "" {
		marker = "env:" + envName + ":" + unitPath
	}

	if err := client.PostOrUpdateComment(projectID, mrIID, marker, formatted); err != nil {
		return fmt.Errorf("posting comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment posted for %s\n", unitPath)
	return nil
}
