---
description: Review the cumulative changes on an AVC branch before merging
argument-hint: [branch-name]
---

Review an AVC branch and report whether it is ready to merge.

1. If `$ARGUMENTS` names a branch, use it. Otherwise call **avc_branch_list** and use the active branch.
2. Call **avc_branch_diff** for that branch.
3. Show the user the full diff.
4. Then give your own read on it:
   - What the branch changes, in one or two sentences
   - Anything that looks unintended — files touched that the task did not call for, build artifacts, secrets, large binaries
   - Whether tests were run on this branch, and what happened

Finish by asking whether to merge. Do **not** call **avc_merge** in this command — merging requires the user to say yes first, in their own words.
