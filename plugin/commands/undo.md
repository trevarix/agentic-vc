---
description: Reverse the last AVC restore or merge
---

Reverse the most recent AVC restore or merge.

1. Call **avc_undo** with no arguments. It reverses the last restore or merge and reports what it undid.
2. Tell the user exactly what was reversed and which snapshot the project is now at.

If **avc_undo** reports there is nothing to undo, say so and stop. Do not go looking for something else to roll back — call **avc_list** and let the user choose a snapshot if they want to go further back.
