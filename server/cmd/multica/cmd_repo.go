package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Work with repositories",
}

var repoCheckoutCmd = &cobra.Command{
	Use:   "checkout <url>",
	Short: "Check out a repository into the working directory",
	Long:  "Creates a git worktree from the daemon's bare clone cache. Used by agents to check out repos on demand.",
	Args:  exactArgs(1),
	RunE:  runRepoCheckout,
}

var repoCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new repository in the workspace",
	RunE:  runRepoCreate,
}

func init() {
	repoCmd.AddCommand(repoCheckoutCmd)
	repoCmd.AddCommand(repoCreateCmd)

	// repo create
	repoCreateCmd.Flags().String("url", "", "Git URL (https or git@); required")
	repoCreateCmd.Flags().String("name", "", "Human-friendly name; required")
	repoCreateCmd.Flags().String("default-branch", "main", "Default branch name")
	repoCreateCmd.Flags().String("description", "", "Optional description")
	repoCreateCmd.Flags().String("platform", "github", "Platform: github | gitlab | other")
	repoCreateCmd.Flags().String("output", "text", "Output format: text or json")
	_ = repoCreateCmd.MarkFlagRequired("url")
	_ = repoCreateCmd.MarkFlagRequired("name")
}

// ---------------------------------------------------------------------------
// repo create
// ---------------------------------------------------------------------------

func runRepoCreate(cmd *cobra.Command, _ []string) error {
	repoURL, _ := cmd.Flags().GetString("url")
	name, _ := cmd.Flags().GetString("name")
	defaultBranch, _ := cmd.Flags().GetString("default-branch")
	description, _ := cmd.Flags().GetString("description")
	platform, _ := cmd.Flags().GetString("platform")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{
		"url":            repoURL,
		"name":           name,
		"default_branch": defaultBranch,
		"description":    description,
		"platform":       platform,
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/repositories", body, &result); err != nil {
		return fmt.Errorf("create repository: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	id := strVal(result, "id")
	resultURL := strVal(result, "url")
	resultName := strVal(result, "name")
	branch := strVal(result, "default_branch")
	fmt.Fprintf(os.Stdout, "Registered %s (%s) — branch %s\n  ID: %s\n", resultName, resultURL, branch, id)
	return nil
}

// ---------------------------------------------------------------------------
// repo checkout
// ---------------------------------------------------------------------------

func runRepoCheckout(cmd *cobra.Command, args []string) error {
	repoURL := args[0]

	daemonPort := os.Getenv("MULTICA_DAEMON_PORT")
	if daemonPort == "" {
		return fmt.Errorf("MULTICA_DAEMON_PORT not set (this command is intended to be run by an agent inside a daemon task)")
	}

	workspaceID := os.Getenv("MULTICA_WORKSPACE_ID")
	agentName := os.Getenv("MULTICA_AGENT_NAME")
	taskID := os.Getenv("MULTICA_TASK_ID")

	// Use current working directory as the checkout target.
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	reqBody := map[string]string{
		"url":          repoURL,
		"workspace_id": workspaceID,
		"workdir":      workDir,
		"agent_name":   agentName,
		"task_id":      taskID,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := httpClient.Post(
		fmt.Sprintf("http://127.0.0.1:%s/repo/checkout", daemonPort),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checkout failed: %s", string(body))
	}

	var result struct {
		Path       string `json:"path"`
		BranchName string `json:"branch_name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Fprintf(os.Stdout, "%s\n", result.Path)
	fmt.Fprintf(os.Stderr, "Checked out %s → %s (branch: %s)\n", repoURL, result.Path, result.BranchName)

	return nil
}
