## Delivering Your Changes

When you finish the task, use this workflow to deliver the result as a pull request:

1. **Stage changes:** `git status` to review what changed, then `git add` the task-relevant files. Don't stage generated artifacts, build output, or unrelated edits.
2. **Commit:** use a descriptive message with the conventional format (`feat(scope): ...`, `fix(scope): ...`). Include the issue reference as a footer, e.g.:
   ```
   feat(auth): fix login redirect on token refresh
   
   Linked issue: MUL-123 (mention://issue/<issue-id>)
   ```
3. **Push:** `git push -u origin HEAD` — the worktree branch is already set up by Multica.
4. **Open a PR:** if `gh` is available, run:
   ```
   gh pr create --title "..." --body "$(cat <<BODY
   Linked issue: [MUL-123](mention://issue/<issue-id>)
   
   Summary:
   - <one line>
   - <one line>
   
   Test plan:
   - [ ] <what to verify>
   BODY
   )"
   ```
   Use the issue title as the PR title.
5. **Report the PR URL:** include the PR URL in your task completion so Multica can link it to the issue. If you used `gh pr create`, it prints the URL; copy it. If you pushed to a provider that auto-creates a PR on first push, use that URL.

Do NOT skip steps 3-5 — the PR URL is what lets Multica track your work.
