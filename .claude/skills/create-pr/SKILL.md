# /create-pr

Draft and open a GitHub pull request for the current branch, following the project's PR conventions and template.

## Usage

```
/create-pr
/create-pr <hint>
```

The optional `<hint>` provides context the diff alone won't reveal (e.g. `/create-pr fixes the race condition reported in issue #42`).

---

## Steps

1. **Verify preconditions**
   - Run `git branch --show-current` to confirm you are not on `main`. If on `main`, stop:
     > You are on main. Create a feature branch first, then run `/create-pr`.
   - Run `git status --short`. If there are uncommitted changes, warn:
     > You have uncommitted changes. Consider running `/create-commit` first.
     Then ask: "Continue anyway or stop?"
   - Check `gh auth status`. If not authenticated, stop and tell the user to run `gh auth login`.

2. **Gather branch context**
   - Run `git log main..HEAD --pretty=format:"%s"` to list commits on this branch.
   - Run `git diff main...HEAD --stat` to get a file-level summary of changes.
   - Run `git diff main...HEAD` to read the full diff (used for drafting the body).
   - Note the branch name — it signals intent (e.g. `feat/annotate-command`, `fix/workspace-path`, `docs/contributing`).

3. **Detect change surfaces**
   From the diff stat, determine which sections of the PR template apply:
   - Any file under `avc/cmd/avc/` or `avc/internal/` → CLI changes apply
   - Any file under `avc/internal/mcp/` → MCP changes apply
   - Any file under `extension/src/` → Extension changes apply
   - Only `.md` files, `.github/`, `.claude/` → Docs / tests only

4. **Draft the PR**

   **Title** — `<type>: <short description>` (under 70 characters, imperative mood, no period)
   - Derive type from branch prefix or dominant change: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `ci`

   **Body** — fill in the PR template (`.github/PULL_REQUEST_TEMPLATE.md`):
   - **Summary**: one or two sentences on what problem this solves and why this approach
   - **Type of change**: tick the matching box(es)
   - **What changed**: brief implementation description; call out non-obvious decisions
   - **CLI / MCP / Extension sections**: include only the sections that apply (omit the rest entirely — don't leave "Skip if no X changes" stubs)
   - **Tests**: pre-tick boxes that the diff evidence supports (e.g. if `*_test.go` files are present, tick the `go test` box)
   - **Docs**: tick if `.md` files were changed in the same branch
   - **Notes for reviewer**: include if there are trade-offs, known gaps, or things to stress-test; omit the section entirely if there is nothing to add
   - If a `<hint>` was provided, use it to fill in context the diff doesn't show

5. **Show the draft**
   Display the full title and body in a code block. Also show:
   - Target branch: `main`
   - Branch being pushed: current branch
   - Any unstaged/uncommitted files excluded from the diff (one-line note)

6. **Confirm and push**
   Ask: "Shall I push this branch and open the PR?"
   - If yes:
     1. Run `git push -u origin <branch>` (only if the branch has no upstream yet — check with `git rev-parse --abbrev-ref @{u}` first).
     2. Check if a PR already exists: `gh pr view --json url 2>/dev/null`.
        - If a PR exists → run `gh pr edit --title "<title>" --body "<body>"` to update it.
        - If no PR exists → run `gh pr create --base <target> --title "<title>" --body "<body>"`.
     3. Print the PR URL.
   - If the user wants to edit the title or body first → apply edits, show updated draft, confirm again.
   - If no → stop without pushing or creating anything.

---

## Rules

- Never force-push (`--force`).
- Never target a branch other than `main` unless the user explicitly says so.
- Never open a draft PR unless the user asks for it.
- Always show the full PR body before creating — no silent submissions.
- If CI failures are visible in the conversation, flag them before asking to proceed.
- Do not include "Skip if no X changes" placeholder lines in the final body — omit irrelevant sections entirely.
- The PR template checklist items should reflect the actual diff: pre-tick what is evidenced, leave unticked what cannot be verified.
