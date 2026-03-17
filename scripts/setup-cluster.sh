#!/usr/bin/env bash
# setup-cluster.sh — Creates a kind cluster with 5 scripted failure scenarios
# for use as a demo bundle source for Autopsy.
#
# Failures:
#   1. OOMKilled:       deployment with 32Mi memory limit + memory-hungry container
#   2. CrashLoopBackOff: pod with invalid entrypoint command
#   3. ImagePullBackOff: deployment referencing nginx:doesnotexist-v999
#   4. Pending:         resource request of 32Gi memory (exceeds any node capacity)
#   5. Missing ConfigMap: deployment with envFrom referencing nonexistent ConfigMap

set -euo pipefail

CLUSTER_NAME="autopsy-demo"

# ── Prerequisites check ────────────────────────────────────────────────────────
for cmd in kind kubectl; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: $cmd is not installed or not in PATH" >&2
    exit 1
  fi
done

echo "==> Creating kind cluster: $CLUSTER_NAME"

# Delete existing cluster if present.
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  echo "    Deleting existing cluster..."
  kind delete cluster --name "$CLUSTER_NAME"
fi

kind create cluster --name "$CLUSTER_NAME" --wait 60s
kubectl cluster-info --context "kind-${CLUSTER_NAME}"

echo ""
echo "==> Deploying failure scenarios..."

# ── Namespace ─────────────────────────────────────────────────────────────────
kubectl create namespace demo

# ── 1. OOMKilled: memory-hungry container with tiny limit ─────────────────────
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: oom-demo
  namespace: demo
  labels:
    scenario: oom-killed
spec:
  replicas: 1
  selector:
    matchLabels:
      app: oom-demo
  template:
    metadata:
      labels:
        app: oom-demo
    spec:
      containers:
      - name: memory-eater
        image: polinux/stress
        command: ["stress"]
        args: ["--vm", "1", "--vm-bytes", "64M", "--vm-keep"]
        resources:
          requests:
            memory: "16Mi"
          limits:
            memory: "32Mi"
EOF

# ── 2. CrashLoopBackOff: bad entrypoint ───────────────────────────────────────
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: crashloop-demo
  namespace: demo
  labels:
    scenario: crash-loop-backoff
spec:
  replicas: 1
  selector:
    matchLabels:
      app: crashloop-demo
  template:
    metadata:
      labels:
        app: crashloop-demo
    spec:
      containers:
      - name: bad-entrypoint
        image: busybox:1.35
        command: ["/bin/sh", "-c"]
        args: ["echo 'startup' && exit 1"]
        resources:
          requests:
            memory: "16Mi"
          limits:
            memory: "32Mi"
EOF

# ── 3. ImagePullBackOff: nonexistent image tag ────────────────────────────────
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: imagepull-demo
  namespace: demo
  labels:
    scenario: image-pull-backoff
spec:
  replicas: 1
  selector:
    matchLabels:
      app: imagepull-demo
  template:
    metadata:
      labels:
        app: imagepull-demo
    spec:
      containers:
      - name: bad-image
        image: nginx:doesnotexist-v999
        resources:
          requests:
            memory: "32Mi"
          limits:
            memory: "64Mi"
EOF

# ── 4. Pending: impossible resource request ───────────────────────────────────
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pending-demo
  namespace: demo
  labels:
    scenario: pending-insufficient-resources
spec:
  replicas: 1
  selector:
    matchLabels:
      app: pending-demo
  template:
    metadata:
      labels:
        app: pending-demo
    spec:
      containers:
      - name: resource-hog
        image: nginx:1.25
        resources:
          requests:
            memory: "32Gi"
          limits:
            memory: "32Gi"
EOF

# ── 5. Missing ConfigMap: envFrom referencing nonexistent ConfigMap ───────────
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: missing-config-demo
  namespace: demo
  labels:
    scenario: missing-configmap
spec:
  replicas: 1
  selector:
    matchLabels:
      app: missing-config-demo
  template:
    metadata:
      labels:
        app: missing-config-demo
    spec:
      containers:
      - name: app
        image: nginx:1.25
        envFrom:
        - configMapRef:
            name: app-config-does-not-exist
        resources:
          requests:
            memory: "32Mi"
          limits:
            memory: "64Mi"
EOF

# ── Healthy workload for contrast ─────────────────────────────────────────────
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: healthy-nginx
  namespace: demo
  labels:
    scenario: healthy
spec:
  replicas: 2
  selector:
    matchLabels:
      app: healthy-nginx
  template:
    metadata:
      labels:
        app: healthy-nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.25
        resources:
          requests:
            memory: "32Mi"
          limits:
            memory: "64Mi"
EOF

echo ""
echo "==> Waiting 90 seconds for failure states to stabilise..."
sleep 90

echo ""
echo "==> Pod status:"
kubectl get pods -n demo -o wide

echo ""
echo "==> Events (last 20):"
kubectl get events -n demo --sort-by='.lastTimestamp' | tail -20

echo ""
echo "==> Cluster is ready. Run 'make demo-bundle' to generate a support bundle."
echo "    Cluster context: kind-${CLUSTER_NAME}"
