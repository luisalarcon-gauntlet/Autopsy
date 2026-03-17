# Autopsy Demo Script — 2-Minute Walkthrough

Use this script for a screen recording or live demo.

---

## Setup (before recording)

1. Start the server: `make dev` OR `docker-compose up`
2. Have the demo bundle ready: `make demo-cluster && make demo-bundle`
3. Open Chrome at `http://localhost:8080`
4. Open browser DevTools → Network tab (to show SSE streams if desired)

---

## Script

### 0:00 — Introduction (15 seconds)

> "This is Autopsy — an AI-powered Kubernetes support bundle analyzer.
> Drop in a `.tar.gz` support bundle, and in about 30 seconds you get
> a triage score, a failure timeline, a root cause analysis, and a chat
> interface grounded in the actual bundle data."

*(Show the upload page)*

---

### 0:15 — Upload the demo bundle (30 seconds)

> "I'll drag in a bundle generated from a kind cluster with 5 scripted
> failures: an OOMKilled workload, a CrashLoopBackOff, an ImagePullBackOff,
> a pod stuck in Pending due to a 32Gi memory request, and a missing
> ConfigMap reference."

*(Drag `demo-bundle-*.tar.gz` onto the drop zone)*

> "The bundle is uploaded, extracted, and parsed — all streaming, nothing
> held fully in memory."

---

### 0:45 — Dashboard loads (30 seconds)

*(Watch panels stream in)*

> "The Risk Scorecard arrives first — severity 85, cluster health critical.
> Notice the top issues: CrashLoopBackOff, OOMKilled, Unschedulable. That
> covers all 5 scenarios."

*(Point to the Timeline panel)*

> "Phase 2 reconstructs the failure sequence chronologically — T+0, the
> first OOM kill; T+8 minutes, the crash loop begins; T+18, the image pull
> fails. This is what happened and when."

*(Watch RCA stream in)*

> "Phase 3 is streaming — tokens arrive live from Claude. Root cause,
> evidence with specific pod names from the bundle, numbered kubectl fix
> commands, and prevention advice."

---

### 1:15 — Chat interface (30 seconds)

*(Click a suggested question pill, e.g. "What is causing X to crash?")*

> "After triage completes, Autopsy suggests relevant questions. I'll click
> this one."

*(Watch the streaming response with typing indicator)*

> "The response streams in and references the actual pod names from the
> bundle. The context injection keeps answers grounded — it won't
> hallucinate resources that aren't in the bundle."

*(Type a follow-up: "Give me all the kubectl commands to remediate this")*

> "And it maintains conversation history across turns."

---

### 1:45 — Wrap up (15 seconds)

> "Upload the same bundle again and the result is instant — SHA256
> content-addressed cache. No API call needed."

> "Run without an API key and it works in stub mode with pre-canned
> responses — great for reviewers who want to kick the tires."

*(Show `http://localhost:8080/debug/cache` briefly)*

> "That's Autopsy. Clone it, run `docker-compose up`, and drop in any
> Replicated Troubleshoot bundle."

---

## Key Talking Points

- **Zero JS build step**: Tailwind CDN + HTMX CDN + vanilla EventSource
- **Memory-safe**: streaming tar.gz extraction, never loads full archive
- **Token budget**: progressive trimming keeps BundleData under 80k tokens
- **Stub mode**: works without an API key for reviewers
- **One-command run**: `docker-compose up`
