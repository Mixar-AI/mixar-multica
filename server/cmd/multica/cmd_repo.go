package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

var repoUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update repository name / description / default branch",
	Args:  cobra.ExactArgs(1),
	RunE:  runRepoUpdate,
}

var repoDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a repository (cascades to worktrees and PRs)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRepoDelete,
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repositories registered in the workspace",
	RunE:  runRepoList,
}

var repoWorktreesCmd = &cobra.Command{
	Use:   "worktrees",
	Short: "List worktrees in the workspace",
	RunE:  runRepoWorktrees,
}

func init() {
	repoCmd.AddCommand(repoCheckoutCmd)
	repoCmd.AddCommand(repoCreateCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	repoCmd.AddCommand(repoDeleteCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoWorktreesCmd)

	// repo create
	repoCreateCmd.Flags().String("url", "", "Git URL (https or git@); required")
	repoCreateCmd.Flags().String("name", "", "Human-friendly name; required")
	repoCreateCmd.Flags().String("default-branch", "main", "Default branch name")
	repoCreateCmd.Flags().String("description", "", "Optional description")
	repoCreateCmd.Flags().String("platform", "github", "Platform: github | gitlab | other")
	repoCreateCmd.Flags().String("output", "text", "Output format: text or json")
	_ = repoCreateCmd.MarkFlagRequired("url")
	_ = repoCreateCmd.MarkFlagRequired("name")

	// repo update
	repoUpdateCmd.Flags().String("name", "", "New name")
	repoUpdateCmd.Flags().String("description", "", "New description")
	repoUpdateCmd.Flags().String("default-branch", "", "New default branch")
	repoUpdateCmd.Flags().String("output", "text", "Output format: text or json")

	// repo list
	repoListCmd.Flags().String("output", "text", "Output format: text or json")

	// repo worktrees
	repoWorktreesCmd.Flags().String("repo", "", "Filter by repository ID")
	repoWorktreesCmd.Flags().Bool("include-inactive", false, "Include inactive/deleted worktrees")
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
// repo update
// ---------------------------------------------------------------------------

func runRepoUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]

	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("name"); v != "" {
		body["name"] = v
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("default-branch"); v != "" {
		body["default_branch"] = v
	}
	if len(body) == 0 {
		return errors.New("at least one of --name / --description / --default-branch is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/repositories/"+id, body, &result); err != nil {
		return fmt.Errorf("update repository: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	resultName := strVal(result, "name")
	resultURL := strVal(result, "url")
	branch := strVal(result, "default_branch")
	fmt.Fprintf(os.Stdout, "Updated %s (%s) — branch %s\n  ID: %s\n", resultName, resultURL, branch, id)
	return nil
}

// ---------------------------------------------------------------------------
// repo delete
// ---------------------------------------------------------------------------

func runRepoDelete(cmd *cobra.Command, args []string) error {
	id := args[0]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/repositories/"+id); err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Deleted repository %s\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// repo list
// ---------------------------------------------------------------------------

func runRepoList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var repos []map[string]any
	if err := client.GetJSON(ctx, "/api/repositories", &repos); err != nil {
		return fmt.Errorf("list repositories: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, repos)
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stdout, "(no repositories registered)")
		return nil
	}
	for _, r := range repos {
		id := strVal(r, "id")
		name := strVal(r, "name")
		repoURL := strVal(r, "url")
		branch := strVal(r, "default_branch")
		fmt.Fprintf(os.Stdout, "%-40s %s (branch %s)\n  ID: %s\n", name, repoURL, branch, id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// repo worktrees
// ---------------------------------------------------------------------------

func runRepoWorktrees(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repoFilter, _ := cmd.Flags().GetString("repo")
	includeInactive, _ := cmd.Flags().GetBool("include-inactive")

	var path string
	if repoFilter != "" {
		path = "/api/repositories/" + repoFilter + "/worktrees"
	} else {
		path = "/api/worktrees"
	}
	if includeInactive {
		path += "?include_inactive=true"
	}

	var worktrees []map[string]any
	if err := client.GetJSON(ctx, path, &worktrees); err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}

	return cli.PrintJSON(os.Stdout, worktrees)
}

// ---------------------------------------------------------------------------
// repo checkout
// ---------------------------------------------------------------------------

func runRepoCheckout(cmd *cobra.Command, args []string) error {
	repoURL := args[0]

	// Ensure the URL is registered in the workspace before attempting checkout.
	client, clientErr := newAPIClient(cmd)
	if clientErr == nil {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer verifyCancel()

		var repos []struct {
			URL string `json:"url"`
		}
		if err := client.GetJSON(verifyCtx, "/api/repositories", &repos); err == nil {
			registered := false
			for _, r := range repos {
				if r.URL == repoURL {
					registered = true
					break
				}
			}
			if !registered {
				return fmt.Errorf("repository %q not registered in workspace; run 'multica repo create --url %s --name <name>' first or add it via workspace settings", repoURL, repoURL)
			}
		}
		// If the server is unreachable, fall through and let the daemon handle errors.
	}

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

	// Forward per-task picker fields injected by the daemon via env vars.
	type checkoutReqBody struct {
		URL           string   `json:"url"`
		WorkspaceID   string   `json:"workspace_id"`
		WorkDir       string   `json:"workdir"`
		AgentName     string   `json:"agent_name"`
		TaskID        string   `json:"task_id"`
		BaseBranch    string   `json:"base_branch,omitempty"`
		ReuseWorktree bool     `json:"reuse_worktree,omitempty"`
		SparsePaths   []string `json:"sparse_paths,omitempty"`
	}

	reqBody := checkoutReqBody{
		URL:           repoURL,
		WorkspaceID:   workspaceID,
		WorkDir:       workDir,
		AgentName:     agentName,
		TaskID:        taskID,
		BaseBranch:    os.Getenv("MULTICA_BASE_BRANCH"),
		ReuseWorktree: os.Getenv("MULTICA_REUSE_WORKTREE") == "true",
	}
	if raw := os.Getenv("MULTICA_SPARSE_PATHS"); raw != "" {
		reqBody.SparsePaths = strings.Split(raw, ",")
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
