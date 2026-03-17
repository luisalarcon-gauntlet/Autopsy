# My Approach and Thoughts

## Why Hierarchical Summarization

The core challenge in this problem is bridging the gap between a noisy, sprawling support bundle (hundreds of files, megabytes of logs) and a 200k-token context window. I chose hierarchical summarization: extract the most actionable signals from raw data, rather than feeding Claude a dump of everything.

The pipeline runs in three phases:
1. **Triage** — fast structured JSON scan: severity score, top issues, affected namespaces
2. **Timeline** — chronological reconstruction from events + log timestamps
3. **RCA** — streaming markdown: root cause, evidence, kubectl fix steps, prevention

Each phase is cheaper than a monolith would be. Triage results seed the chat suggestions, so the UI feels reactive to the actual cluster state.

## The Token Budget Problem

Support bundles can contain thousands of pod log lines. Naïvely including them all would blow the context. My solution is a multi-pass `EnforceBudget` function in `internal/bundle/budget.go` that progressively trims:

1. Truncate log excerpts to 20 lines each
2. Drop healthy pods (only keep pods with `Reason != ""`)
3. Drop Normal events entirely
4. Truncate event messages to 100 chars

I only extract logs for pods with restarts or failed phase (no noise from healthy containers), and I use smart extraction: last 30 lines + any line containing `ERROR`/`FATAL`/`panic`. This keeps the signal-to-noise ratio high.

## Why SSE + HTMX Over WebSockets or React

WebSockets are bidirectional — overkill for server-push analysis results. SSE is simpler: one HTTP connection, browser-native EventSource API, reconnects automatically. It maps cleanly to the streaming Claude API, which delivers tokens as they arrive.

HTMX removes the JS build pipeline entirely. Each SSE panel is one HTML element with `hx-ext="sse"`. The server sends complete HTML partials rather than JSON that the client must render — which means the template logic stays in Go where it's typed and testable, not scattered across a React component tree. The entire frontend is ~250 lines of HTML and ~100 lines of vanilla JavaScript.

The tradeoff: HTMX is less composable than React for complex state, and the streaming chat required dropping back to a native EventSource + DOM manipulation for the typing indicator. Acceptable for this scope.

## Interesting Observations

Pending pods with insufficient memory are tricky: they have no container statuses at all (the pod was never scheduled), so you must look at `status.conditions[type=PodScheduled]`. This is documented in `internal/bundle/NOTES.md`.

## What I'd Do Next

- **Persistent storage**: Currently sessions are in-memory and ephemeral. A SQLite backend would allow resuming past analyses.
- **Multi-bundle comparison**: "What changed between this bundle and the last one?" — a diff view of triage scores and new issues would be very useful for incident post-mortems.
- **ISV-specific analyzer configs**: Different products have different failure patterns. A `autopsy.yaml` config could tune the prompts and parsers for specific Kubernetes distributions or application stacks.
- **Automated remediation**: The RCA already outputs `kubectl` commands. With RBAC-scoped cluster access, Autopsy could offer one-click apply for simple fixes.
