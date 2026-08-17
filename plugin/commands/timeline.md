---
description: Show what agents have done on this project, session by session
---

Run `avc timeline --json` and summarize it for the user.

Group the output by session. For each session, report:

- The task, and when it started
- How many snapshots it produced
- Which files it touched most
- Whether it ended in a merge, a restore, or neither

Lead with anything that looks unfinished — a session with snapshots but no merge, or one that ended in a restore. Those are the ones the user most likely wants to act on.

Keep it to a short readable summary. Do not paste the raw JSON.
