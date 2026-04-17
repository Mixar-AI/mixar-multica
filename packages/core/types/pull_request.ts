export interface PullRequest {
  id: string;
  workspace_id: string;
  repository_id: string;
  issue_id?: string;
  task_id?: string;
  pr_url: string;
  pr_number: number;
  head_branch: string;
  base_branch: string;
  state: string;
  title: string;
  created_by_agent_id?: string;
  created_at: string;
  last_synced_at?: string;
}
