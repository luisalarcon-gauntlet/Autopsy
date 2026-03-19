// Package seed inserts demo bundles and analyses for the Airbyte org
// so the customer detail trend graphs have data on first boot.
package seed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yourusername/autopsy/internal/db"
)

type seedEntry struct {
	id            string
	customerName  string
	filename      string
	uploadedAt    time.Time
	severity      int
	clusterHealth string
	topIssue      string
	timelineJSON  string
	rcaMarkdown   string
}

// backticks replaces ~~~ with ``` and ~~ with ` so raw string literals can contain code fences and inline code.
func backticks(s string) string {
	s = strings.ReplaceAll(s, "~~~", "```")
	s = strings.ReplaceAll(s, "~~", "`")
	return s
}

var entries = []seedEntry{
	// ── Toyota — worsening trend ────────────────────────────────────────────
	{
		id:            "seed-toyota-001",
		customerName:  "Toyota",
		filename:      "toyota-cluster-2024-01-01.tar.gz",
		uploadedAt:    date("2024-01-01"),
		severity:      34,
		clusterHealth: "healthy",
		topIssue:      "Minor memory pressure on worker pods",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster bootstrapped normally","detail":"All 3 nodes registered and passed readiness checks. 18 of 18 pods running across 4 namespaces.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+8:00","title":"Worker node memory utilization elevated","detail":"node-worker-1 reached 74% memory utilization. No pressure condition set yet, but approaching the 80% threshold.","severity":"warning","linkedPod":""},` +
			`{"relativeTime":"T+22:00","title":"airbyte-worker approaching memory limit","detail":"airbyte-worker-5b9d7f8c4-kp2qx consuming 420Mi of its 512Mi limit (82%). Pod is healthy and running but operating with minimal headroom.","severity":"warning","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+60:00","title":"Cluster stable — no escalation","detail":"Memory utilization did not escalate further. No OOMKill events, no pod restarts. Cluster remained healthy for the observation window.","severity":"info","linkedPod":""}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Healthy — minor memory pressure, no failures
**Critical pods:** 0 failing, 18 healthy
**Root cause confidence:** High
**Estimated fix time:** 10 minutes

## Root Cause
The airbyte-worker pod is operating at 82% of its configured 512Mi memory limit, leaving only ~92Mi of headroom. Node-worker-1 is at 74% overall memory utilization. While the cluster is healthy today, continued workload growth without a corresponding increase in limits risks OOMKills in the near future.

## Evidence
- ~~node-worker-1~~: 74% memory utilization (pressure threshold: 80%)
- ~~airbyte-worker-5b9d7f8c4-kp2qx~~: 420Mi / 512Mi memory (82% of limit)
- 0 pod restarts recorded in this bundle
- 0 Warning events in the event log

## Fix Steps
1. Increase the airbyte-worker memory limit before the next bundle sync:
~~~bash
kubectl -n airbyte set resources deployment airbyte-worker \
  --limits=memory=768Mi --requests=memory=512Mi
~~~
2. Monitor node memory with: ~~kubectl top nodes~~
3. Consider enabling VPA for automatic limit tuning

## Patch Files
~~~yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: airbyte-worker
  namespace: airbyte
spec:
  template:
    spec:
      containers:
      - name: airbyte-worker
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "768Mi"
            cpu: "1000m"
~~~

## Prevention
- Set node memory alerts at 75% utilization to catch pressure before OOMKills occur
- Review pod resource utilization weekly with ~~kubectl top pods -n airbyte~~
- Enable VPA to automatically tune resource limits based on observed usage`),
	},

	{
		id:            "seed-toyota-002",
		customerName:  "Toyota",
		filename:      "toyota-cluster-2024-01-08.tar.gz",
		uploadedAt:    date("2024-01-08"),
		severity:      61,
		clusterHealth: "warning",
		topIssue:      "CrashLoopBackOff on airbyte-worker, 3 restarts",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster started, memory pressure pre-existing","detail":"node-worker-1 already at 79% memory utilization from prior week. airbyte-worker pod scheduled successfully.","severity":"warning","linkedPod":""},` +
			`{"relativeTime":"T+12:00","title":"airbyte-worker first restart","detail":"airbyte-worker-5b9d7f8c4-kp2qx terminated unexpectedly (exit code 137 — OOMKilled). Restart #1 initiated automatically.","severity":"warning","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+28:00","title":"CrashLoopBackOff begins after 3 restarts","detail":"Pod entered CrashLoopBackOff state. Back-off timer is now 40s between restart attempts. Sync jobs unable to execute.","severity":"critical","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+45:00","title":"Back-off delay at maximum (5m)","detail":"Restart back-off reached 5m0s ceiling. Airbyte sync connections are stalled. Dependent pipelines failing with timeout errors.","severity":"critical","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+67:00","title":"Pod temporarily stabilized after manual restart","detail":"Operator restarted the deployment. Pod came up successfully but memory utilization immediately climbed to 91% of limit.","severity":"warning","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Degraded — worker pod in CrashLoopBackOff, sync paused
**Critical pods:** 1 failing (airbyte-worker), 17 healthy
**Root cause confidence:** High
**Estimated fix time:** 20 minutes

## Root Cause
The airbyte-worker pod is being OOMKilled on startup because its memory limit (512Mi) is insufficient for the current workload. The pod initializes successfully but then crashes when memory consumption exceeds the cap during active sync operations. Each restart triggers the Kubernetes exponential back-off timer, eventually halting all Airbyte data pipelines.

## Evidence
- ~~airbyte-worker-5b9d7f8c4-kp2qx~~: 3 restarts, reason: OOMKilled (exit code 137)
- CrashLoopBackOff state confirmed — back-off at 5m0s maximum
- ~~node-worker-1~~: 79% memory utilization at time of incident
- Event: ~~OOMKilling — Memory limit of container airbyte-worker exceeded~~
- Sync jobs failing with timeout after worker unavailability

## Fix Steps
1. Immediately increase the memory limit to stop the crash loop:
~~~bash
kubectl -n airbyte set resources deployment airbyte-worker \
  --limits=memory=1Gi --requests=memory=768Mi
~~~
2. Restart the deployment to clear the back-off:
~~~bash
kubectl -n airbyte rollout restart deployment/airbyte-worker
~~~
3. Watch the rollout:
~~~bash
kubectl -n airbyte rollout status deployment/airbyte-worker --timeout=120s
~~~

## Patch Files
~~~yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: airbyte-worker
  namespace: airbyte
spec:
  template:
    spec:
      containers:
      - name: airbyte-worker
        resources:
          requests:
            memory: "768Mi"
            cpu: "500m"
          limits:
            memory: "1Gi"
            cpu: "2000m"
~~~

## Prevention
- Set memory requests equal to the P90 observed usage, and limits 50% above requests
- Configure PodDisruptionBudget to maintain at least 1 worker replica during node maintenance
- Alert on CrashLoopBackOff immediately — do not wait for the back-off to self-heal`),
	},

	{
		id:            "seed-toyota-003",
		customerName:  "Toyota",
		filename:      "toyota-cluster-2024-01-15.tar.gz",
		uploadedAt:    date("2024-01-15"),
		severity:      85,
		clusterHealth: "critical",
		topIssue:      "OOMKilled memory-hog, 14 restarts — critical",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster started with pre-existing OOM history","detail":"airbyte-worker arrived from prior week with 3 prior OOMKill events. Node memory at 81% — MemoryPressure condition active on node-worker-1.","severity":"warning","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+8:00","title":"OOMKill #4 — worker terminated again","detail":"airbyte-worker-5b9d7f8c4-kp2qx killed by the kernel OOM reaper. Container used 514Mi against 512Mi hard limit at moment of kill.","severity":"critical","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+22:00","title":"Restart storm: 10 OOMKill events in 14 minutes","detail":"Pod cycling every 60-90 seconds. Kubernetes scheduler is unable to reschedule to another node — all nodes are memory-pressured.","severity":"critical","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+41:00","title":"14 total OOMKills — back-off at maximum","detail":"Pod has now restarted 14 times. CrashLoopBackOff back-off is at 5m ceiling. All Airbyte sync connections are stalled.","severity":"critical","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"},` +
			`{"relativeTime":"T+55:00","title":"Airbyte worker offline — pipelines failing","detail":"Worker has been unreachable for >10 minutes. Dependent data pipelines are failing. Manual intervention required to restore service.","severity":"critical","linkedPod":"airbyte-worker-5b9d7f8c4-kp2qx"}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Critical — airbyte-worker OOMKilled 14 times, all syncs down
**Critical pods:** 1 failing (airbyte-worker), 17 healthy
**Root cause confidence:** High
**Estimated fix time:** 15 minutes

## Root Cause
The airbyte-worker pod has a hard memory limit of 512Mi but requires significantly more memory during active sync operations. The pod is killed by the Linux kernel OOM reaper every time it approaches the ceiling, then re-enters CrashLoopBackOff. The underlying cause is insufficient memory allocation combined with possible memory growth over recent Airbyte version updates. With all worker nodes also under MemoryPressure, the pod cannot be rescheduled to a healthier node.

## Evidence
- ~~airbyte-worker-5b9d7f8c4-kp2qx~~: 14 restarts, all OOMKilled (exit code 137)
- Peak memory usage at kill time: 514Mi (over 512Mi limit by 2Mi)
- Event log: 14x ~~OOMKilling — Memory limit of container airbyte-worker exceeded~~
- ~~node-worker-1~~: MemoryPressure condition True, 81% utilization
- All Airbyte sync connections reporting timeout errors

## Fix Steps
1. Immediately increase the memory limit and restart the worker:
~~~bash
kubectl -n airbyte set resources deployment airbyte-worker \
  --limits=memory=2Gi --requests=memory=1Gi
kubectl -n airbyte rollout restart deployment/airbyte-worker
~~~
2. Verify the pod comes up clean:
~~~bash
kubectl -n airbyte get pods -w
kubectl -n airbyte logs -f deployment/airbyte-worker --since=5m
~~~
3. Clear the MemoryPressure condition by evicting low-priority pods from node-worker-1:
~~~bash
kubectl drain node-worker-1 --ignore-daemonsets --delete-emptydir-data --grace-period=30
~~~

## Patch Files
~~~yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: airbyte-worker
  namespace: airbyte
spec:
  template:
    spec:
      containers:
      - name: airbyte-worker
        resources:
          requests:
            memory: "1Gi"
            cpu: "1000m"
          limits:
            memory: "2Gi"
            cpu: "2000m"
~~~
~~~yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: airbyte-worker-pdb
  namespace: airbyte
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: airbyte-worker
~~~

## Prevention
- The memory limit should be at least 2× the P99 observed peak usage
- Add a cluster-level alert: CrashLoopBackOff for >2 minutes = PagerDuty P1
- Schedule weekly node capacity reviews — MemoryPressure on any node is a signal to scale
- Pin Airbyte worker versions and validate memory requirements before upgrading`),
	},

	// ── Nike — stable and healthy ───────────────────────────────────────────
	{
		id:            "seed-nike-001",
		customerName:  "Nike",
		filename:      "nike-cluster-2024-01-05.tar.gz",
		uploadedAt:    date("2024-01-05"),
		severity:      15,
		clusterHealth: "healthy",
		topIssue:      "",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster healthy — all nodes ready","detail":"3/3 nodes in Ready state. 22 pods scheduled and running. No Warning events in the past 48 hours.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+10:00","title":"Resource utilization nominal","detail":"CPU at 18%, memory at 39% across all nodes. All Airbyte connectors running and reporting successful syncs.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+20:00","title":"No incidents detected — cluster stable","detail":"Analysis complete. Zero pod restarts, zero warning events, zero OOMKill events. Cluster is operating well within capacity.","severity":"info","linkedPod":""}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Healthy — no issues detected
**Critical pods:** 0 failing, 22 healthy
**Root cause confidence:** High
**Estimated fix time:** 0 minutes — no action required

## Root Cause
No root cause to identify. The cluster is operating normally within all resource thresholds. All Airbyte sync connectors are healthy and reporting successful pipeline completions.

## Evidence
- All 3 nodes in Ready state, zero MemoryPressure or DiskPressure conditions
- CPU utilization: 18% cluster-wide
- Memory utilization: 39% cluster-wide
- 0 pod restarts across all namespaces
- 0 Warning events in the past 48 hours

## Fix Steps
No remediation required. Continue monitoring.

## Patch Files
No patches required — cluster is healthy.

## Prevention
- Continue current resource allocation strategy
- Schedule quarterly capacity reviews to plan ahead for workload growth
- Maintain Airbyte connector versions on a regular update cadence`),
	},

	{
		id:            "seed-nike-002",
		customerName:  "Nike",
		filename:      "nike-cluster-2024-01-10.tar.gz",
		uploadedAt:    date("2024-01-10"),
		severity:      12,
		clusterHealth: "healthy",
		topIssue:      "",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"All nodes ready, workloads healthy","detail":"3/3 nodes Ready. 22 pods running. CPU 15%, memory 37%. No change from prior bundle.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+15:00","title":"Sync jobs completing successfully","detail":"All configured Airbyte connectors reported successful sync completions in the past 24 hours. No failures or timeouts.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+25:00","title":"Cluster stable — no incidents","detail":"Analysis window complete. Resource utilization trending slightly down since last bundle. No action needed.","severity":"info","linkedPod":""}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Healthy — resource utilization trending down
**Critical pods:** 0 failing, 22 healthy
**Root cause confidence:** High
**Estimated fix time:** 0 minutes — no action required

## Root Cause
No issues detected. Cluster resource utilization has trended slightly downward since the Jan 5 bundle. All sync connectors are operating normally.

## Evidence
- CPU utilization: 15% (down from 18% in prior bundle)
- Memory utilization: 37% (down from 39% in prior bundle)
- 0 pod restarts
- 0 Warning events
- All Airbyte sync jobs completed successfully

## Fix Steps
No remediation required.

## Patch Files
No patches required — cluster is healthy.

## Prevention
- Continue monitoring. Positive trend — resource utilization decreasing while workload remains stable suggests recent optimizations are working.`),
	},

	{
		id:            "seed-nike-003",
		customerName:  "Nike",
		filename:      "nike-cluster-2024-01-15.tar.gz",
		uploadedAt:    date("2024-01-15"),
		severity:      12,
		clusterHealth: "healthy",
		topIssue:      "",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster healthy — consistent baseline","detail":"3/3 nodes Ready. 23 pods running (1 new connector deployed). CPU 16%, memory 41%.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+12:00","title":"New connector deployed cleanly","detail":"airbyte-source-salesforce-7c4f9d8b2-r3kxw deployed and reached Running state within 45 seconds. Memory within expected range.","severity":"info","linkedPod":"airbyte-source-salesforce-7c4f9d8b2-r3kxw"},` +
			`{"relativeTime":"T+22:00","title":"All connectors running, no issues","detail":"Cluster steady. New connector completing its first sync run successfully. No warning events.","severity":"info","linkedPod":""}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Healthy — new connector deployed successfully
**Critical pods:** 0 failing, 23 healthy
**Root cause confidence:** High
**Estimated fix time:** 0 minutes — no action required

## Root Cause
No issues. The cluster absorbed a new Salesforce connector deployment without any resource contention or instability. Workload is growing steadily and cluster capacity remains comfortable.

## Evidence
- 23 pods running (23/23 healthy), including 1 new connector
- CPU utilization: 16%, memory: 41% — within healthy range
- New pod ~~airbyte-source-salesforce-7c4f9d8b2-r3kxw~~ running, 0 restarts
- 0 Warning events

## Fix Steps
No remediation required.

## Patch Files
No patches required — cluster is healthy.

## Prevention
- Plan for capacity review at ~60% memory utilization to maintain comfortable headroom as connectors grow`),
	},

	// ── Goldman Sachs — improving trend ────────────────────────────────────
	{
		id:            "seed-goldman-001",
		customerName:  "Goldman Sachs",
		filename:      "goldman-cluster-2024-01-03.tar.gz",
		uploadedAt:    date("2024-01-03"),
		severity:      71,
		clusterHealth: "critical",
		topIssue:      "PostgreSQL connection pool exhausted",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster started — services nominal","detail":"All nodes Ready. airbyte-server, airbyte-worker, and postgres pods running. 25 active connections to PostgreSQL (pool size: 100).","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+15:00","title":"PostgreSQL connections surging","detail":"Active connections reached 87/100. Connection surge correlates with a batch of 14 newly triggered sync jobs. No errors yet.","severity":"warning","linkedPod":"airbyte-db-6c8d7f9b4-m2nvx"},` +
			`{"relativeTime":"T+22:00","title":"Connection pool exhausted — all 100 slots in use","detail":"PostgreSQL connection pool hit the hard limit. New connection requests from airbyte-server are being rejected with 'too many clients' error.","severity":"critical","linkedPod":"airbyte-db-6c8d7f9b4-m2nvx"},` +
			`{"relativeTime":"T+28:00","title":"API 503s — airbyte-server returning errors","detail":"airbyte-server unable to acquire DB connections for API requests. All /api/v1/* endpoints returning HTTP 503. Users cannot access the UI.","severity":"critical","linkedPod":"airbyte-server-4d6e5b7c3-k8pwz"},` +
			`{"relativeTime":"T+45:00","title":"Pool partially recovered after connection timeouts","detail":"Idle connections timed out and returned to pool. Error rate dropped to 35%. Service partially restored but still degraded.","severity":"warning","linkedPod":"airbyte-db-6c8d7f9b4-m2nvx"}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Critical — PostgreSQL connection pool exhausted, API returning 503s
**Critical pods:** 1 degraded (airbyte-db), 24 healthy
**Root cause confidence:** High
**Estimated fix time:** 30 minutes

## Root Cause
A batch of 14 simultaneous Airbyte sync jobs each opened multiple PostgreSQL connections, exhausting the 100-connection pool limit. Once the pool was full, airbyte-server could not acquire connections to handle API requests, causing all endpoints to return HTTP 503. The root cause is a combination of: (1) an undersized connection pool for concurrent workload, and (2) lack of connection pooling middleware (e.g. PgBouncer) to multiplex connections efficiently.

## Evidence
- PostgreSQL event: ~~too many clients already~~ — 100/100 connections in use
- ~~airbyte-server-4d6e5b7c3-k8pwz~~: API error rate 100% at peak, HTTP 503 on all endpoints
- 14 sync jobs triggered simultaneously — each job opens 3-7 connections
- Connection pool size: 100 (PostgreSQL ~~max_connections~~)
- No connection timeout was configured — idle connections held indefinitely

## Fix Steps
1. Immediately kill long-running idle connections to restore service:
~~~bash
kubectl exec -n airbyte airbyte-db-6c8d7f9b4-m2nvx -- psql -U airbyte -c \
  "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE state = 'idle' AND query_start < NOW() - INTERVAL '5 minutes';"
~~~
2. Increase max_connections in PostgreSQL ConfigMap:
~~~bash
kubectl -n airbyte edit configmap airbyte-db-config
# Set: max_connections = 200
kubectl -n airbyte rollout restart deployment/airbyte-db
~~~
3. Deploy PgBouncer as a connection pooler in front of PostgreSQL

## Patch Files
~~~yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: airbyte-db-config
  namespace: airbyte
data:
  POSTGRES_MAX_CONNECTIONS: "200"
  POSTGRES_IDLE_TIMEOUT: "300"
  POSTGRES_CONNECTION_TIMEOUT: "30"
~~~

## Prevention
- Deploy PgBouncer in transaction-pooling mode to multiplex thousands of app connections onto a small pool of real DB connections
- Set connection limits per Airbyte role using PostgreSQL ~~ALTER ROLE ... CONNECTION LIMIT~~
- Stagger large batch sync job triggers to avoid simultaneous connection storms
- Alert when PostgreSQL active connections exceed 70% of max_connections`),
	},

	{
		id:            "seed-goldman-002",
		customerName:  "Goldman Sachs",
		filename:      "goldman-cluster-2024-01-10.tar.gz",
		uploadedAt:    date("2024-01-10"),
		severity:      52,
		clusterHealth: "warning",
		topIssue:      "Elevated error rate on api-server",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster stable after Jan 3 incident — PgBouncer deployed","detail":"PostgreSQL connections healthy at 28/200 (post-fix). airbyte-server running normally. PgBouncer deployed as connection proxy.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+18:00","title":"Error rate rising on airbyte-server","detail":"HTTP 5xx rate on airbyte-server climbed from 0.3% baseline to 8.1% over 20 minutes. No obvious trigger — no new sync jobs or deployments.","severity":"warning","linkedPod":"airbyte-server-4d6e5b7c3-k8pwz"},` +
			`{"relativeTime":"T+31:00","title":"Repeated 500s on /api/v1/connections endpoint","detail":"Specific endpoint /api/v1/connections returning HTTP 500 with error: 'Failed to deserialize connection configuration'. Likely a schema migration side-effect.","severity":"warning","linkedPod":"airbyte-server-4d6e5b7c3-k8pwz"},` +
			`{"relativeTime":"T+52:00","title":"Error rate dropped after pod restart","detail":"Operator restarted airbyte-server deployment. Error rate fell from 8.1% to 1.2% within 90 seconds. Root cause likely memory fragmentation or stale in-memory cache.","severity":"info","linkedPod":"airbyte-server-4d6e5b7c3-k8pwz"}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Warning — airbyte-server error rate elevated, partially resolved by restart
**Critical pods:** 0 critical, 1 degraded (airbyte-server), 24 healthy
**Root cause confidence:** Medium
**Estimated fix time:** 15 minutes

## Root Cause
The airbyte-server pod developed an elevated error rate (8.1%) on the ~~/api/v1/connections~~ endpoint. The errors — ~~Failed to deserialize connection configuration~~ — suggest a schema mismatch between the in-memory connection cache and the database representation, possibly caused by a previous schema migration. A pod restart resolved the immediate symptoms, but the underlying schema migration process should be reviewed to prevent recurrence.

## Evidence
- ~~airbyte-server-4d6e5b7c3-k8pwz~~: 8.1% HTTP 5xx error rate (baseline: 0.3%)
- Error message: ~~Failed to deserialize connection configuration~~ on ~~/api/v1/connections~~
- Error onset was gradual (20 minutes), not a sudden spike — suggests cache poisoning not a crash
- Pod restart reduced errors from 8.1% → 1.2%
- PostgreSQL connections healthy (28/200) — DB is not the issue

## Fix Steps
1. If error rate is still above 2%, restart the server deployment:
~~~bash
kubectl -n airbyte rollout restart deployment/airbyte-server
kubectl -n airbyte rollout status deployment/airbyte-server
~~~
2. Check for pending schema migrations:
~~~bash
kubectl exec -n airbyte airbyte-db-6c8d7f9b4-m2nvx -- psql -U airbyte -c "\dt" | grep migration
~~~
3. Review the airbyte-server logs around T+18:00 for the initial error trigger

## Patch Files
~~~yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: airbyte-server
  namespace: airbyte
spec:
  template:
    spec:
      containers:
      - name: airbyte-server
        livenessProbe:
          httpGet:
            path: /api/v1/health
            port: 8001
          initialDelaySeconds: 30
          periodSeconds: 10
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /api/v1/health
            port: 8001
          initialDelaySeconds: 10
          periodSeconds: 5
~~~

## Prevention
- Add liveness and readiness probes to airbyte-server so Kubernetes auto-restarts degraded pods
- Run schema migrations in a pre-upgrade hook, not alongside the application
- Alert on sustained 5xx rate >2% for more than 5 minutes`),
	},

	{
		id:            "seed-goldman-003",
		customerName:  "Goldman Sachs",
		filename:      "goldman-cluster-2024-01-15.tar.gz",
		uploadedAt:    date("2024-01-15"),
		severity:      44,
		clusterHealth: "warning",
		topIssue:      "Memory pressure on 2 nodes — monitoring",
		timelineJSON: `{"events":[` +
			`{"relativeTime":"T+0:00","title":"Cluster operational — services healthy","detail":"All 4 nodes Ready, 27 pods running. airbyte-server error rate back to 0.4% baseline after Jan 10 fix.","severity":"info","linkedPod":""},` +
			`{"relativeTime":"T+12:00","title":"node-worker-2 memory at 81% — MemoryPressure set","detail":"Kubernetes set MemoryPressure=True on node-worker-2. No pod evictions yet but scheduler will avoid placing new pods on this node.","severity":"warning","linkedPod":""},` +
			`{"relativeTime":"T+28:00","title":"node-worker-3 memory approaching threshold","detail":"node-worker-3 reached 79% memory utilization. If it crosses 80%, a second node will enter MemoryPressure, limiting scheduling capacity.","severity":"warning","linkedPod":""},` +
			`{"relativeTime":"T+45:00","title":"No evictions — cluster stable under pressure","detail":"Both nodes holding below eviction threshold. No pods evicted. Monitoring is active. Headroom is limited if workload grows.","severity":"info","linkedPod":""}` +
			`]}`,
		rcaMarkdown: backticks(`## TL;DR
**Cluster health:** Warning — 2 of 4 nodes under memory pressure, no evictions yet
**Critical pods:** 0 failing, 27 healthy
**Root cause confidence:** High
**Estimated fix time:** 20 minutes to scale; 0 minutes if just monitoring

## Root Cause
Two worker nodes (node-worker-2 at 81%, node-worker-3 at 79%) are approaching or exceeding the Kubernetes memory pressure threshold. The pressure is driven by gradual workload growth following the addition of new Airbyte connectors in recent weeks. No pods have been evicted yet, but if memory utilization continues to grow, Kubernetes will begin evicting pods to reclaim memory, which could disrupt active syncs.

## Evidence
- ~~node-worker-2~~: 81% memory utilization — MemoryPressure condition True
- ~~node-worker-3~~: 79% memory utilization — approaching 80% threshold
- No pod evictions recorded in this bundle
- 27 pods running, all healthy — pressure is node-level, not pod-level yet
- Cluster has been growing steadily: 22 pods (Jan 5) → 27 pods (Jan 15)

## Fix Steps
1. Add a new worker node to the cluster to relieve pressure:
~~~bash
# Scale node pool (adjust for your cloud provider)
gcloud container clusters resize goldman-airbyte-prod --node-pool default-pool --num-nodes 5
# OR for AWS EKS:
aws eks update-nodegroup-config --cluster-name goldman-airbyte-prod \
  --nodegroup-name workers --scaling-config minSize=3,maxSize=6,desiredSize=5
~~~
2. While waiting for the new node, identify the highest-memory pods and check for optimization opportunities:
~~~bash
kubectl top pods -n airbyte --sort-by=memory
~~~

## Patch Files
~~~yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: airbyte-worker-hpa
  namespace: airbyte
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: airbyte-worker
  minReplicas: 2
  maxReplicas: 6
  metrics:
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 70
~~~

## Prevention
- Set cluster autoscaler to scale out when node memory utilization exceeds 70%
- Alert at 75% node memory — not 80% (too late by the time MemoryPressure is set)
- Track pod count growth week-over-week; plan node capacity additions proactively`),
	},
}

// Run seeds demo bundles for the airbyte org. All operations are idempotent —
// bundle inserts use ON CONFLICT DO NOTHING, analysis upserts overwrite.
func Run(ctx context.Context, d *db.DB) {
	ok := 0
	for _, e := range entries {
		if err := seedOne(ctx, d, e); err != nil {
			slog.Warn("seed: failed", "id", e.id, "err", err)
		} else {
			ok++
		}
	}
	slog.Info("seed: demo data ready", "entries", len(entries), "ok", ok)
}

func seedOne(ctx context.Context, d *db.DB, e seedEntry) error {
	// Build triage JSON.
	topIssues := []map[string]any{}
	if e.topIssue != "" {
		topIssues = append(topIssues, map[string]any{
			"title":       e.topIssue,
			"severity":    healthToIssueSeverity(e.clusterHealth),
			"affectedPod": "",
			"category":    "unknown",
		})
	}
	triageJSON, err := json.Marshal(map[string]any{
		"severityScore":      e.severity,
		"clusterHealth":      e.clusterHealth,
		"summary":            fmt.Sprintf("Cluster health: %s. Severity score: %d.", e.clusterHealth, e.severity),
		"topIssues":          topIssues,
		"affectedNamespaces": []string{"airbyte"},
	})
	if err != nil {
		return fmt.Errorf("marshal triage: %w", err)
	}

	// Deterministic SHA256 from the seed ID so dedup never fires on real uploads.
	sum := sha256.Sum256([]byte(e.id))
	sha256hex := hex.EncodeToString(sum[:])

	// Bundle insert is idempotent (ON CONFLICT DO NOTHING).
	if err := d.InsertBundleWithTime(ctx, db.Bundle{
		ID:            e.id,
		OrgID:         "airbyte",
		CustomerName:  e.customerName,
		Filename:      e.filename,
		FileSizeBytes: 0,
		SHA256:        sha256hex,
		UploadedBy:    "seed",
		FileData:      []byte{},
		UploadedAt:    e.uploadedAt,
	}); err != nil {
		return fmt.Errorf("insert bundle: %w", err)
	}

	// All analysis upserts overwrite — safe to run on every startup.
	if err := d.SaveTriage(ctx, e.id, e.severity, e.clusterHealth, string(triageJSON)); err != nil {
		return fmt.Errorf("save triage: %w", err)
	}
	if e.timelineJSON != "" {
		if err := d.SaveTimeline(ctx, e.id, e.timelineJSON); err != nil {
			return fmt.Errorf("save timeline: %w", err)
		}
	}
	if e.rcaMarkdown != "" {
		if err := d.SaveRCA(ctx, e.id, e.rcaMarkdown); err != nil {
			return fmt.Errorf("save rca: %w", err)
		}
	}
	return nil
}

func date(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func healthToIssueSeverity(health string) string {
	switch health {
	case "critical":
		return "critical"
	case "warning":
		return "high"
	default:
		return "low"
	}
}
