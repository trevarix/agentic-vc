# /create-changelog

Prepare the CHANGELOG.md for a release by moving `[Unreleased]` entries into a versioned section, populated from git commit history since the last tag.

## Usage

```
/create-changelog <version>
```

Example: `/create-changelog 0.3.0`

---

## Steps

When this skill is invoked with a version argument (e.g. `0.3.0`):

1. **Validate input**
   - The argument must be a valid semver string (`MAJOR.MINOR.PATCH` optionally with pre-release suffix, no leading `v`).
   - If missing or malformed, stop and ask the user to provide it.

2. **Check CHANGELOG.md**
   - Read `CHANGELOG.md` from the project root.
   - Confirm an `## [Unreleased]` section exists. If it doesn't, stop and tell the user.

3. **Gather commits since last tag**
   - Run: `git describe --tags --abbrev=0` to find the most recent tag.
   - If no tags exist, gather all commits: `git log --pretty=format:"%s"`.
   - Otherwise: `git log <last-tag>..HEAD --pretty=format:"%s"` to get commit subjects since that tag.
   - Filter out merge commits (lines starting with `Merge`).

4. **Detect release type**
   - If no prior tags exist, this is a **first release**. Apply the special first-release rules in step 5.
   - Otherwise treat it as a **regular release**.

5. **Build the entry list**

   **For a regular release:**
   Map conventional commit prefixes to Keep a Changelog categories:
   - `feat:` / `feat(...):`    → `### Added`
   - `fix:` / `fix(...):`      → `### Fixed`
   - `refactor:` / `perf:`     → `### Changed`
   - `docs:`                   → `### Changed`
   - `chore:` / `ci:` / `test:` → `### Changed`
   - `remove:` / `deprecate:`  → `### Removed`
   - Anything else             → `### Changed`

   Strip the conventional commit prefix from each message before listing (e.g. `feat: add annotate command` → `- Add annotate command`). Capitalise the first word.

   Group related commits: if multiple commits clearly describe the same feature or fix, collapse them into one well-worded entry rather than listing each individually.

   **For a first release:**
   - Do not mechanically list one entry per commit. Instead, synthesise the commits into a coherent set of feature descriptions that tell readers what the product does.
   - Lead with an "Initial release of <product name> — <one-line description>" entry under `### Added`.
   - Use inaugural language throughout: "Introduce", "Add", "Build", "Launch". Avoid words that imply a prior version existed ("Improved", "Redesigned", "Refreshed", "Overhaul", "Updated") — these read as if something existed before that was worse.
   - Group related commits (e.g. all VSCode extension commits become one or two entries, all CI commits become one entry).
   - Still include all meaningful work — do not skip commits. If a commit is minor housekeeping, fold it into a broader entry rather than omitting it.
   - Place genuine bug fixes (`fix:`) under `### Fixed` even on a first release.

6. **Build the new section**
   ```
   ## [<version>] - <today's date YYYY-MM-DD>

   ### Added
   - ...

   ### Fixed
   - ...

   ### Changed
   - ...
   ```
   Omit any subsection that has no entries.

   If there are no commits since the last tag (or no commits at all), write:
   ```
   ## [<version>] - <today's date YYYY-MM-DD>

   No changes logged. Update this section before releasing.
   ```

7. **Update CHANGELOG.md**
   - Replace the `## [Unreleased]` heading (and its contents, if any) with the new versioned section.
   - Insert a fresh empty `## [Unreleased]` block above the new section:
     ```
     ## [Unreleased]

     ## [<version>] - <date>
     ...
     ```
   - Write the file. Do not change any other part of CHANGELOG.md.

8. **Show a summary**
   - Display the new versioned section to the user.
   - Remind them to review and edit before tagging:
     > Review the entries above, edit as needed, then tag with `git tag v<version>` to trigger the release workflow.

---

## Rules

- Never commit, stage, or tag. Write the file only.
- Never modify anything in CHANGELOG.md outside the `[Unreleased]` block.
- Always use today's date (available in system context) for the release date.
- If a commit message is ambiguous or uncategorisable, put it under `### Changed` rather than dropping it.
- Deduplicate entries that appear more than once (same subject after prefix stripping).
- **Write release-note quality prose throughout** — for every release, not just the first. Entries must be ready to publish as-is: clear to a user or stakeholder who has never seen the commit history, free of internal jargon, developer shorthand, ticket references, and raw commit phrasing. Ask yourself "would this make sense in a public release announcement?" before writing each line.
- Prefer describing the user-facing outcome over the technical action (e.g. "Branches now isolate agent changes from your main project" over "Add workspace isolation to branch command").
