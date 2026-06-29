# /create-release

Cut a release by validating the changelog, tagging the commit, and pushing the tag to trigger the automated release workflow (cross-platform binaries + VSCode `.vsix` + GitHub Release).

## Usage

```
/create-release <version>
```

Example: `/create-release 0.3.0`

The version must not include a leading `v` — the skill adds it when creating the tag.

---

## How the automated release works

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which:
1. Builds CLI binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
2. Packages the VSCode extension as a `.vsix`
3. Creates a GitHub Release with all artifacts and the changelog section as the release body

This skill's job is to validate everything is ready, then push the tag.

---

## Steps

1. **Validate the version argument**
   - Must match `MAJOR.MINOR.PATCH` (optionally with pre-release suffix like `-beta.1`), no leading `v`.
   - If missing or malformed, stop and ask the user to provide a valid semver string.

2. **Verify preconditions**

   *Branch:*
   - Run `git branch --show-current`. Must be `main`. If not, stop:
     > Releases must be cut from main. Switch to main, merge your branch, then run `/create-release` again.

   *Working tree:*
   - Run `git status --short`. If there are uncommitted changes, stop:
     > You have uncommitted changes. Run `/create-commit` to commit them, then retry.

   *Remote sync:*
   - Run `git fetch origin main --dry-run` then `git rev-list HEAD..origin/main --count`.
   - If `origin/main` is ahead, stop:
     > Your local main is behind origin/main. Run `git pull` first.
   - If local main is ahead of origin, warn and ask: "Local main has unpushed commits. Push them before tagging? (yes/no)"
     - If yes: run `git push origin main`, then continue.
     - If no: stop.

   *Tag collision:*
   - Run `git tag -l "v<version>"`. If the tag already exists, stop:
     > Tag v<version> already exists. Choose a different version or delete the existing tag first.

   *GitHub CLI:*
   - Run `gh auth status`. If not authenticated, stop and tell the user to run `gh auth login`.

3. **Validate the changelog**
   - Read `CHANGELOG.md`.
   - Check that a `## [<version>]` section exists (e.g. `## [0.3.0]`).
   - If the section is missing, stop:
     > CHANGELOG.md has no section for version <version>.
     > Run `/create-changelog <version>` to generate it, review the entries, then retry.
   - If the section exists but contains only "No changes logged", warn:
     > The changelog section for <version> appears empty. Consider updating it before releasing.
     > Continue anyway? (yes/no)
   - If `## [Unreleased]` still has content below it (non-empty), warn:
     > There are unreleased entries above the [<version>] section that won't be included in this release.
     > Continue anyway? (yes/no)

4. **Show release summary**
   Display a confirmation block:
   ```
   Release: v<version>
   Branch:  main (<short SHA> — <first line of HEAD commit>)
   Tag:     v<version>  →  triggers release.yml

   Changelog section:
   <contents of the ## [<version>] section>

   Artifacts that will be built by CI:
   - avc-linux-amd64
   - avc-linux-arm64
   - avc-darwin-amd64
   - avc-darwin-arm64
   - avc-windows-amd64.exe
   - avc-<version>.vsix
   ```

5. **Confirm and tag**
   Ask: "Shall I create and push tag v<version>?"
   - If yes:
     1. Run `git tag -a "v<version>" -m "Release v<version>"`.
     2. Run `git push origin "v<version>"`.
     3. Report success and print the Actions URL:
        > Tag v<version> pushed. Release workflow running at:
        > https://github.com/trevarix/agentic-vc/actions
        > Monitor progress there, or run `gh run list --workflow=release.yml` to check status.
   - If no → stop. The tag has not been created.

---

## Rules

- Never tag from any branch other than `main`.
- Never use `git push --force` on a tag.
- Never delete or move an existing tag.
- Never proceed past a failed precondition — each one must be explicitly resolved.
- Always show the full release summary and wait for an explicit yes before tagging.
- If the release workflow fails after tagging, instruct the user to check GitHub Actions and delete the tag with `git tag -d v<version> && git push origin :refs/tags/v<version>` before retrying — do not attempt to fix CI issues automatically.
