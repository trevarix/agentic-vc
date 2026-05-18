# /create-commit

Draft and create a git commit for staged changes, following the project's conventional commit style.

## Usage

```
/create-commit
/create-commit <hint>
```

The optional `<hint>` gives context about intent (e.g. `/create-commit fix the branch workspace path bug`). Use it when the diff alone doesn't make the intent obvious.

---

## Steps

1. **Check staged changes**
   - Run `git diff --staged --stat` to see what is staged.
   - If nothing is staged, stop and tell the user:
     > Nothing is staged. Stage your changes with `git add <files>` then run `/commit` again.
   - Do not auto-stage anything.

2. **Read the full diff**
   - Run `git diff --staged` to read the actual changes.
   - Also run `git status --short` to see any unstaged or untracked files that are NOT included — mention them briefly so the user can decide whether to stage them too.

3. **Draft the commit message**

   **Subject line** (`<type>: <short description>`):
   - Pick one type from: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `ci`, `perf`
   - Keep the subject under 70 characters
   - Use the imperative mood: "add", "fix", "remove" — not "added", "fixes", "removed"
   - Do not end with a period
   - If a `<hint>` was provided, let it guide the type and description

   **Body** (optional, separated by a blank line):
   - Include only if the *why* is non-obvious from the subject
   - Explain motivation and context, not what the code does
   - Wrap at 72 characters

   **Project-specific rules:**
   - Changes only in `.github/`, `docs/`, `CONTRIBUTING.md`, `README.md`, `CHANGELOG.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md` → use `docs:` or `ci:`
   - Changes in `avc/cmd/avc/` paired with `avc/internal/` → usually `feat:` or `fix:`
   - Changes only in `avc/tests/` or `*_test.go` → use `test:`
   - Changes in `.claude/` → use `chore:`
   - Mixed concerns in one commit → pick the dominant type; note the rest in the body

4. **Show the draft**
   Present the full commit message in a code block for review:
   ```
   <type>: <short description>

   <optional body>
   ```
   Also list any unstaged/untracked files that were excluded, with a one-line note on whether they seem related.

5. **Ask for confirmation**
   Ask: "Shall I commit with this message, or would you like to edit it?"
   - If the user confirms → run `git commit -m "<message>"` and report the short SHA.
   - If the user edits → apply their edits and commit.
   - If the user says no → stop without committing.

---

## Rules

- Never run `git add`, `git push`, or `git tag` — only `git commit`.
- Never use `--no-verify` or skip hooks.
- Never commit if `go test ./...` or `npm run compile` failures are visible in the conversation — flag it instead.
- Always show the full commit message before committing. No silent commits.
- If the staged diff contains secrets (API keys, tokens, `.env` content) — stop and warn the user before doing anything else.
