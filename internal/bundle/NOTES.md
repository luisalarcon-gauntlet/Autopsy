# Bundle Structure Notes

Observations about Kubernetes support bundle formats encountered during S6.2 validation.

## Directory Layout (Replicated Troubleshoot)

Typical structure produced by `kubectl support-bundle`:

```
<bundle-root>/
  cluster-resources/
    nodes.json                    # NodeList
    <namespace>/
      pods.json                   # PodList for that namespace
      events.json                 # EventList for that namespace
  logs/
    <namespace>/
      <pod-name>/
        <container-name>.log      # Raw container log text
  helm/
    releases.json                 # Helm release list (if Helm is used)
  cluster-info/
    cluster_version.json          # {"gitVersion":"v1.28.0"}
  version/
    version.json                  # Alternate version location
```

## Failure Mode Observations

### 1. OOMKilled
- Pod phase: `Running` or `CrashLoopBackOff`
- `status.containerStatuses[].lastState.terminated.reason = "OOMKilled"`
- `status.containerStatuses[].state.waiting.reason = "CrashLoopBackOff"` (after enough restarts)
- Parser priority: `state.waiting.reason` wins over `lastState.terminated.reason`; may show CrashLoopBackOff rather than OOMKilled as the "current" reason

### 2. CrashLoopBackOff
- Pod phase: `Running`
- `status.containerStatuses[].state.waiting.reason = "CrashLoopBackOff"`
- Exit code in `lastState.terminated.exitCode` (non-zero)

### 3. ImagePullBackOff
- Pod phase: `Pending`
- `status.containerStatuses[].state.waiting.reason = "ImagePullBackOff"` or `"ErrImagePull"`
- Message contains the registry URL and the "not found" or "access denied" detail

### 4. Pending (Insufficient Resources)
- Pod phase: `Pending`
- No container statuses (pod never scheduled)
- `status.conditions[type="PodScheduled"].status = "False"`
- `status.conditions[type="PodScheduled"].reason = "Unschedulable"`
- `status.conditions[type="PodScheduled"].message` = "0/N nodes are available: N Insufficient memory..."
- **Parser fix (S6.2):** Added pod condition parsing for Pending pods to surface the Unschedulable reason

### 5. Missing ConfigMap (CreateContainerConfigError)
- Pod phase: `Pending`
- `status.containerStatuses[].state.waiting.reason = "CreateContainerConfigError"`
- Message: "couldn't find key X in ConfigMap <namespace>/<name>"

## Edge Cases

- **Events path:** Some bundles omit per-namespace event files; the parser handles missing files gracefully
- **Timestamps:** Both `firstTimestamp` and `eventTime` (microsecond precision) appear in different bundle versions; the events parser tries both
- **Helm:** Not all bundles include Helm releases; the parser silently skips missing helm paths
- **Log path:** Log directory layout `logs/<namespace>/<pod>/<container>.log` is standard for `kubectl support-bundle`, but some custom collectors may differ
- **Cluster version:** Found in 3 different locations depending on the collection tool; all are checked
