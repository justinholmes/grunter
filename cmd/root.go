package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "grunter",
	Short: "GitOps IaC pipeline for GitLab",
	Long:  "Grunter orchestrates Terragrunt/Terraform deployments via GitLab CI, detecting changes, resolving dependencies, and posting plan results to Merge Requests.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("config", "c", ".grunter/config.yml", "path to config file")
}
