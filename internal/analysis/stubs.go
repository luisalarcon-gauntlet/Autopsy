// Package analysis provides the multi-phase AI analysis pipeline for Autopsy.
package analysis

// StubTriageJSON is a pre-canned JSON triage result for stub mode.
// It models a cluster experiencing both a CrashLoopBackOff and an OOMKilled scenario.
const StubTriageJSON = `{
  "severityScore": 72,
  "summary": "Cluster is in a degraded state. Two workloads are actively failing: the payment-processor deployment is stuck in CrashLoopBackOff and the data-ingestion job has been OOMKilled three times in the past hour.",
  "clusterHealth": "degraded",
  "affectedNamespaces": ["payments", "data-pipeline"],
  "topIssues": [
    {
      "title": "payment-processor in CrashLoopBackOff",
      "severity": "critical",
      "affectedPod": "payment-processor-7d9f5b8c4-xk2pq",
      "category": "crash-loop"
    },
    {
      "title": "data-ingestion-worker OOMKilled repeatedly",
      "severity": "high",
      "affectedPod": "data-ingestion-worker-6b4d9c7f5-m3nrs",
      "category": "oom"
    },
    {
      "title": "redis-cache ImagePullBackOff on canary",
      "severity": "medium",
      "affectedPod": "redis-cache-canary-5f8b4d2c9-p7qwt",
      "category": "image-pull"
    }
  ]
}`

// StubTimelineEvents is a pre-canned set of timeline events for stub mode.
// Events are separated by roughly 10 minutes to simulate a plausible failure cascade.
var StubTimelineEvents = []map[string]string{
	{
		"relativeTime": "T+0:00",
		"title":        "data-ingestion-worker first OOMKilled",
		"detail":       "Container exceeded its 256Mi memory limit. Kubernetes sent SIGKILL. The pod was rescheduled within 30 seconds.",
		"severity":     "warning",
		"linkedPod":    "data-ingestion-worker-6b4d9c7f5-m3nrs",
	},
	{
		"relativeTime": "T+8:14",
		"title":        "payment-processor begins crash loop",
		"detail":       "After the third restart, the back-off timer kicked in. Logs show a nil pointer dereference in the database connection pool initializer.",
		"severity":     "critical",
		"linkedPod":    "payment-processor-7d9f5b8c4-xk2pq",
	},
	{
		"relativeTime": "T+18:42",
		"title":        "redis-cache canary ImagePullBackOff",
		"detail":       "Image redis:7.2-canary-20240315 not found in registry. The canary rollout was attempted automatically by the deployment controller.",
		"severity":     "warning",
		"linkedPod":    "redis-cache-canary-5f8b4d2c9-p7qwt",
	},
	{
		"relativeTime": "T+28:05",
		"title":        "data-ingestion-worker OOMKilled a second time",
		"detail":       "Memory usage spiked to 290Mi before the OOM killer acted. The ingestion batch size appears to have grown since the last deploy.",
		"severity":     "warning",
		"linkedPod":    "data-ingestion-worker-6b4d9c7f5-m3nrs",
	},
	{
		"relativeTime": "T+41:30",
		"title":        "payment-processor back-off reaches 5 minutes",
		"detail":       "CrashLoopBackOff back-off timer is now at 5m0s. Payments are impacted. The node hosting this pod has no scheduling pressure.",
		"severity":     "critical",
		"linkedPod":    "payment-processor-7d9f5b8c4-xk2pq",
	},
}

// StubRCAText is a pre-canned root cause analysis in markdown for stub mode.
// It references the same pods and namespaces used in the triage and timeline stubs.
const StubRCAText = `## Root Cause

The primary failure is a **nil pointer dereference** in the payment-processor service (namespace: payments) occurring during database connection pool initialization. The process crashes immediately on startup, which triggers the CrashLoopBackOff state.

A secondary contributing issue is the data-ingestion-worker (namespace: data-pipeline) consistently exceeding its 256Mi memory limit. Review of recent commits shows the batch processing size was increased from 500 to 5000 records in the v2.3.1 deploy without a corresponding increase in the memory limit.

## Evidence

- Pod ` + "`payment-processor-7d9f5b8c4-xk2pq`" + ` exit code **2** (signal: SIGSEGV) with back-off at 5m0s
- Log line: ` + "`FATAL: runtime error: invalid memory address or nil pointer dereference`" + ` at ` + "`pkg/db/pool.go:47`" + `
- Kubernetes event: ` + "`BackOff pulling image \"redis:7.2-canary-20240315\"`" + ` — image does not exist in the configured registry
- data-ingestion-worker memory usage peaked at **290Mi** before OOM kill (limit: 256Mi)
- 3 OOMKilled events in the last hour for ` + "`data-ingestion-worker-6b4d9c7f5-m3nrs`" + `

## Fix Steps

1. **Fix the nil pointer in payment-processor:**
` + "```bash" + `
# Check the current crash logs
kubectl logs payment-processor-7d9f5b8c4-xk2pq -n payments --previous

# The fix is in pkg/db/pool.go:47 — ensure DB_HOST env var is set
kubectl describe deployment payment-processor -n payments | grep -A5 Env

# Apply a config fix (set the missing env var)
kubectl set env deployment/payment-processor DB_HOST=postgres-service.payments.svc.cluster.local -n payments
` + "```" + `

2. **Increase memory limit for data-ingestion-worker:**
` + "```bash" + `
kubectl patch deployment data-ingestion-worker -n data-pipeline \
  --type=json \
  -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/memory", "value": "512Mi"}]'
` + "```" + `

3. **Fix the redis canary image reference:**
` + "```bash" + `
kubectl set image deployment/redis-cache-canary redis=redis:7.2 -n payments
` + "```" + `

4. **Verify recovery:**
` + "```bash" + `
kubectl rollout status deployment/payment-processor -n payments
kubectl get pods -n payments -w
kubectl get pods -n data-pipeline -w
` + "```" + `

## Prevention

- Add a **liveness probe** to payment-processor that validates DB connectivity on startup to surface missing config before crash loops start
- Set **memory requests equal to limits** for the data-ingestion-worker to guarantee QoS class Guaranteed, preventing node-level OOM eviction
- Pin canary image tags to a verified digest rather than a mutable tag, and validate image existence in CI before deploying
- Add a **PodDisruptionBudget** to the payments namespace so canary rollouts cannot simultaneously take down all replicas
`
