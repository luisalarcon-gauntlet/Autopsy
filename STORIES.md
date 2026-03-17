# Autopsy — Stories

Every story = one git commit. Cursor works on one story at a time.
Format: `feat(SX.Y): description`

Stories are ordered. Do not skip ahead. Do not combine stories into one commit.

---

## Epic 0 — Scaffold & Dev Environment
*Goal: Anyone can clone and run `make dev` within 5 minutes.*

### S0.1 — Go module + air watcher
**Commit:** `chore(S0.1): initialize go module and air live-reload`

Tasks:
- `go mod init github.com/yourusername/autopsy`
- Add `github.com/anthropics/anthropic-sdk-go` dependency
- Create `.air.toml` with correct build cmd and watch patterns
- Create `main.go` with a single `"/"` handler that returns `"Autopsy is running"`
- Create `Makefile` with targets: `dev` (runs air), `build`, `test`, `lint`
- Verify: `make dev` starts a server on :8080 and hot-reloads on file save

Acceptance:
- [ ] `go build ./...` succeeds with zero errors
- [ ] `curl localhost:8080` returns 200

---

### S0.2 — Environment config + startup validation
**Commit:** `chore(S0.2): env config, API key guard, stub mode flag`

Tasks:
- Create `.env.example` with: `ANTHROPIC_API_KEY=`, `PORT=8080`, `MAX_BUNDLE_MB=250`, `STUB_MODE=false`, `SESSION_TTL_MINUTES=30`
- Create `internal/config/config.go` that reads env vars with sane defaults
- At startup: if `ANTHROPIC_API_KEY` is empty, log warning and set `cfg.StubMode = true`
- Expose `cfg` as an immutable value (not global pointer) passed to handlers

Acceptance:
- [ ] App starts without `ANTHROPIC_API_KEY` set (stub mode warning logged)
- [ ] Config values are logged at startup (mask the API key to last 4 chars)

---

### S0.3 — .cursorrules confirmation + project README skeleton
**Commit:** `docs(S0.3): README skeleton and cursor rules acknowledgment`

Tasks:
- Create `README.md` with: project description, prerequisites, quickstart (`make dev` AND `docker-compose up`), env vars table, architecture diagram placeholder
- Create `MY_APPROACH_AND_THOUGHTS.md` skeleton (fill in at end)
- Confirm `.cursor/rules/main.mdc` and this `STORIES.md` are in repo root

Acceptance:
- [ ] README renders correctly on GitHub
- [ ] Both cursor rule files committed

---

### S0.4 — Stub mode stubs file
**Commit:** `chore(S0.4): analysis stub responses for no-API-key mode`

Tasks:
- Create `internal/analysis/stubs.go` with realistic hardcoded responses:
  - `StubTriageResult`: a JSON triage with severity=72, 3 issues, 2 namespaces
  - `StubTimelineEvents`: 5 timeline events with timestamps 10min apart
  - `StubRCAText`: a multi-paragraph root cause analysis mentioning specific pods
- Stubs should look like real analysis of a CrashLoopBackOff + OOMKilled scenario

Acceptance:
- [ ] Stubs compile and are valid JSON where applicable
- [ ] Stubs reference realistic K8s resource names (not "foo" / "bar")

---

## Epic 1 — Bundle Ingestion (Safe & Robust)
*Goal: Accept a .tar.gz upload, extract it safely, clean it up. No OOM, no goroutine leaks.*

### S1.1 — Upload handler with size limit
**Commit:** `feat(S1.1): multipart upload handler with 250MB size limit`

Tasks:
- Create `internal/server/handlers.go` with `HandleUpload(w, r)`:
  - Accept `POST /upload` with `multipart/form-data`
  - Enforce `MAX_BUNDLE_MB` limit via `http.MaxBytesReader`
  - Validate file extension is `.tar.gz` or `.tgz`
  - Return HTTP 413 with JSON error if too large
  - Return HTTP 400 with JSON error for wrong file type
- Create `templates/upload.html`: drag-drop zone, HTMX `hx-post="/upload"`, progress indicator
- Create `templates/layout.html`: base layout with Tailwind + HTMX CDN tags

Acceptance:
- [ ] Upload a 1MB tar.gz → 200 response
- [ ] Upload a non-.tar.gz file → 400 response with clear message
- [ ] Uploading a file over limit → 413 response

---

### S1.2 — Streaming tar.gz extraction (memory-safe)
**Commit:** `feat(S1.2): streaming tar.gz extraction to temp dir`

Tasks:
- Create `internal/bundle/extract.go` with `Extract(ctx context.Context, r io.Reader, maxBytes int64) (string, error)`:
  - Opens a gzip reader on the stream, then tar reader — never fully buffers
  - Creates a temp dir with `os.MkdirTemp`
  - Extracts files, enforcing: max individual file size = 50MB, max total = 500MB
  - Sanitizes paths (prevent path traversal: `filepath.Clean`, reject `..`)
  - Returns the temp dir path on success
- Write `internal/bundle/extract_test.go` using a crafted in-memory tar.gz

Acceptance:
- [ ] `go test -race ./internal/bundle/` passes
- [ ] Path traversal attempt (`../../../../etc/passwd`) is rejected
- [ ] Extracting a legitimate bundle returns a valid temp dir

---

### S1.3 — Session store with TTL cleanup
**Commit:** `feat(S1.3): in-memory session store with TTL goroutine`

Tasks:
- Create `internal/session/store.go`:
  - `Session` struct: `ID string`, `BundleDir string`, `BundleData *bundle.BundleData`, `Analysis *analysis.Result`, `CreatedAt time.Time`, `ChatHistory []ChatMessage`
  - `Store` struct with `sync.RWMutex` protecting a `map[string]*Session`
  - `New(bundleDir string) *Session` generates a UUID session ID
  - `Get(id string) (*Session, bool)`
  - `Set(id string, s *Session)`
  - `Delete(id string)` — also calls `os.RemoveAll(s.BundleDir)`
  - Background goroutine: every 5 minutes, delete sessions older than TTL
- Write `internal/session/store_test.go` testing concurrent access and TTL expiry

Acceptance:
- [ ] `go test -race ./internal/session/` passes (race detector clean)
- [ ] Expired sessions have their temp dirs deleted

---

### S1.4 — Wire upload → extract → session into handler
**Commit:** `feat(S1.4): upload pipeline wired end-to-end`

Tasks:
- Update `HandleUpload` to:
  1. Extract the uploaded file via `bundle.Extract()`
  2. Create a new session, store `BundleDir`
  3. Return HTMX redirect (`HX-Redirect` header) to `/report/{sessionID}`
- Add `GET /report/{sessionID}` handler that renders `templates/report.html` (can be empty shell for now)
- Handle extraction errors gracefully: show error partial, clean up temp dir

Acceptance:
- [ ] Upload a real .tar.gz → redirected to `/report/{uuid}` 
- [ ] Session exists in store after upload
- [ ] Temp dir is cleaned up if extraction fails

---

## Epic 2 — Bundle Parser
*Goal: Turn raw extracted files into a structured BundleData that fits in Claude's context.*

### S2.1 — Pod status and node parser
**Commit:** `feat(S2.1): parse pod status, restart counts, and node conditions`

Tasks:
- Create `internal/bundle/types.go`:
  ```go
  type BundleData struct {
      ClusterVersion  string
      NodeSummaries   []NodeSummary
      PodSummaries    []PodSummary
      Events          []ClusterEvent
      LogExcerpts     []LogExcerpt
      HelmReleases    []HelmRelease
      ParseErrors     []string
      TokenEstimate   int
  }
  type PodSummary struct {
      Namespace    string
      Name         string
      Phase        string   // Running, Pending, Failed, etc.
      Ready        string   // "1/1", "0/1"
      RestartCount int
      Reason       string   // CrashLoopBackOff, OOMKilled, ImagePullBackOff, etc.
      Message      string
      NodeName     string
  }
  type NodeSummary struct {
      Name       string
      Ready      bool
      Conditions []string // NotReady reasons
      Capacity   map[string]string
  }
  ```
- Create `internal/bundle/parser.go` with `Parse(ctx context.Context, bundleDir string) (*BundleData, error)`:
  - Walk `cluster-resources/` directory
  - Parse `pods.json` in each namespace → populate `PodSummaries`
  - Parse `nodes.json` → populate `NodeSummaries`
  - Handle missing files gracefully (append to `ParseErrors`, continue)
- Write fixtures in `internal/bundle/testdata/` (minimal valid pods.json)
- Write parser tests

Acceptance:
- [ ] Parser returns valid BundleData for minimal fixture
- [ ] Missing files logged to ParseErrors, not fatal
- [ ] `go test -race ./internal/bundle/` passes

---

### S2.2 — Events parser with timestamp sorting
**Commit:** `feat(S2.2): parse Kubernetes events with chronological sorting`

Tasks:
- Extend `types.go`:
  ```go
  type ClusterEvent struct {
      Timestamp   time.Time
      Namespace   string
      Name        string
      Kind        string
      Reason      string   // BackOff, OOMKilling, Failed, etc.
      Message     string
      Count       int
      Type        string   // Warning, Normal
  }
  ```
- Add `parseEvents(bundleDir string) ([]ClusterEvent, error)` to parser:
  - Walk `cluster-resources/*/events.json`
  - Parse each event, extract `firstTimestamp` or `eventTime`
  - Sort all events chronologically
  - Deduplicate high-count repeated events (keep first + last + count)
  - Focus on Warning events; include Normal only if count > 10

Acceptance:
- [ ] Events are sorted oldest-first
- [ ] Deduplication keeps the most recent occurrence of repeated events
- [ ] Test with sample events.json fixture

---

### S2.3 — Log excerpt extractor with smart truncation
**Commit:** `feat(S2.3): extract error log excerpts with line budget`

Tasks:
- Extend `types.go`:
  ```go
  type LogExcerpt struct {
      Namespace  string
      PodName    string
      Container  string
      Lines      []string   // max 50 lines
      Truncated  bool
      ErrorCount int
  }
  ```
- Add `extractLogs(bundleDir string, pods []PodSummary) ([]LogExcerpt, error)`:
  - Only extract logs for pods with RestartCount > 0 OR Phase == Failed
  - Read log files from `logs/{namespace}/{pod}/{container}.log`
  - Smart extraction: last 30 lines + any line containing ERROR/FATAL/panic/exception (up to 20 more)
  - Hard cap: 50 lines per container, 10 containers max
  - Track `Truncated = true` if log was cut
- Add `ParseErrors` entries for missing log files (not fatal)

Acceptance:
- [ ] Logs only extracted for unhealthy pods
- [ ] Total log lines across all excerpts <= 500
- [ ] Test with a sample log file containing errors

---

### S2.4 — Token budget enforcer
**Commit:** `feat(S2.4): token budget estimator and enforcer`

Tasks:
- Add to `parser.go`:
  ```go
  func EstimateTokens(data *BundleData) int
  func EnforceBudget(data *BundleData, maxTokens int) *BundleData
  ```
- `EstimateTokens`: rough estimate = total character count / 4
- `EnforceBudget`: if over budget, progressively trim:
  1. Truncate log excerpts to 20 lines each
  2. Drop healthy pods from PodSummaries (keep only Reason != "")
  3. Drop Normal events entirely
  4. Truncate event messages to 100 chars
  5. Repeat until under budget
- Log a warning if budget enforcement was triggered
- Hard limit: 80,000 tokens (≈320,000 chars)

Acceptance:
- [ ] A 500MB bundle doesn't produce a BundleData over 80k tokens
- [ ] Test: generate large fake BundleData, assert EnforceBudget trims correctly
- [ ] `TokenEstimate` field is set on the returned struct

---

## Epic 3 — AI Analysis Pipeline
*Goal: Three-phase analysis with error handling, caching, and stub mode.*

### S3.1 — Phase 1: Triage (structured JSON output)
**Commit:** `feat(S3.1): phase 1 triage analysis with JSON output`

Tasks:
- Create `internal/analysis/types.go`:
  ```go
  type TriageResult struct {
      SeverityScore   int              // 0-100
      Summary         string           // 1-2 sentence overview
      TopIssues       []Issue
      AffectedNS      []string
      ClusterHealth   string           // "critical", "degraded", "warning", "healthy"
  }
  type Issue struct {
      Title       string
      Severity    string   // "critical", "high", "medium", "low"
      AffectedPod string
      Category    string   // "oom", "image-pull", "crash-loop", "config", "resource", "network"
  }
  ```
- Create `internal/analysis/prompts.go` with `TriageSystemPrompt` and `BuildTriagePrompt(data *bundle.BundleData) string`
- Create `internal/analysis/analyzer.go` with `RunTriage(ctx, client, data) (*TriageResult, error)`:
  - Calls Claude with JSON mode instruction
  - Parses JSON response into TriageResult
  - In stub mode: returns StubTriageResult after 500ms delay
- Write `analysis/prompts_test.go` asserting prompt length is under token limit

Acceptance:
- [ ] RunTriage returns valid TriageResult for stub data
- [ ] JSON parsing handles missing fields gracefully
- [ ] Prompt is < 40,000 chars

---

### S3.2 — Phase 2: Timeline reconstruction
**Commit:** `feat(S3.2): phase 2 failure timeline reconstruction`

Tasks:
- Add to `types.go`:
  ```go
  type TimelineResult struct {
      Events []TimelineEvent
  }
  type TimelineEvent struct {
      RelativeTime string   // "T+0:00", "T+3:42", etc.
      Title        string   // "nginx-deployment enters CrashLoopBackOff"
      Detail       string   // 1-2 sentences
      Severity     string   // "info", "warning", "critical"
      LinkedPod    string   // optional
  }
  ```
- Add `BuildTimelinePrompt(data *bundle.BundleData) string` to prompts.go
- Add `RunTimeline(ctx, client, data) (*TimelineResult, error)` to analyzer.go:
  - Ask Claude to reconstruct a chronological narrative from events + log timestamps
  - Return JSON array of TimelineEvents
  - In stub mode: returns StubTimelineResult

Acceptance:
- [ ] Timeline events are ordered chronologically
- [ ] At least one event has severity "critical" in stub data
- [ ] JSON parses cleanly

---

### S3.3 — Phase 3: Root cause analysis (streaming)
**Commit:** `feat(S3.3): phase 3 streaming RCA with kubectl remediation steps`

Tasks:
- Add `RunRCA(ctx context.Context, client *anthropic.Client, data *bundle.BundleData, w io.Writer) error`:
  - Uses `client.Messages.NewStreaming()` for token-by-token output
  - Writes each text delta to `w` as it arrives
  - The prompt instructs Claude to output structured markdown:
    - `## Root Cause` section
    - `## Evidence` section with specific pod/event references
    - `## Fix Steps` section with numbered kubectl commands
    - `## Prevention` section
  - In stub mode: writes StubRCAText to w in 50-char chunks with 30ms delay each
- The `w io.Writer` will be the SSE writer in production

Acceptance:
- [ ] Streaming works — output arrives incrementally
- [ ] Stub mode simulates streaming correctly
- [ ] Context cancellation stops streaming immediately

---

### S3.4 — API error handling + analysis result cache
**Commit:** `feat(S3.4): Claude API error handling and SHA256 result cache`

Tasks:
- Add `internal/analysis/cache.go`:
  - `Cache` struct with `sync.RWMutex` and `map[string]*CachedResult`
  - Key = SHA256 of the raw .tar.gz bytes (computed during upload)
  - `CachedResult` stores all 3 phase results + timestamp
  - Cache TTL = 2 hours
- Wrap all Claude API calls with retry: 1 retry on 529 (overload) with 2s backoff
- Error types to handle gracefully:
  - 429 rate limit → "Analysis rate limited, please retry in 60s"
  - 529 overload → retry once, then "Analysis temporarily unavailable"
  - Network error → "Could not reach analysis service"
  - JSON parse error → "Analysis returned unexpected format"
- Store SHA256 in the session during upload

Acceptance:
- [ ] Second upload of same bundle uses cache (no API call)
- [ ] Simulated 429 shows user-friendly message not a stack trace
- [ ] Cache respects TTL

---

## Epic 4 — Web UI + SSE
*Goal: A dashboard that feels alive — panels stream in as analysis completes.*

### S4.1 — Upload page with drag-drop zone
**Commit:** `feat(S4.1): polished upload page with drag-drop and validation`

Tasks:
- Flesh out `templates/upload.html`:
  - Centered card with dashed border drop zone
  - File input hidden, triggered by clicking the zone
  - Drag-over state (border color change via Tailwind)
  - Client-side validation: file extension, file size (show warning before upload)
  - HTMX: `hx-post="/upload"` `hx-encoding="multipart/form-data"` `hx-indicator="#spinner"`
  - Upload progress bar using HTMX `htmx:xhr:progress` event
  - Error display area (HTMX swaps error partial here)
- Stub mode banner: yellow bar at top if stub mode active

Acceptance:
- [ ] Drag and drop a .tar.gz → upload initiates
- [ ] Dropping a .pdf → client-side error, no upload
- [ ] Upload progress bar visible during transfer
- [ ] Looks polished on 1440px and 1280px screens

---

### S4.2 — Report dashboard layout (4-panel grid)
**Commit:** `feat(S4.2): 4-panel report dashboard with loading states`

Tasks:
- Flesh out `templates/report.html`:
  - Header: "BundleIQ Analysis" + session ID badge + "New Analysis" link
  - 2x2 grid layout:
    - Top-left: Risk Scorecard (connects to `/stream/{id}/triage` SSE)
    - Top-right: Failure Timeline (connects to `/stream/{id}/timeline` SSE)  
    - Bottom-left: Root Cause Analysis (connects to `/stream/{id}/rca` SSE)
    - Bottom-right: Chat Interface (static form)
  - Each panel has a skeleton/loading state while SSE is connecting
  - Each panel has a title, icon, and content area
- Create `templates/partials/risk_card.html`, `timeline.html`, `rca.html` with loading skeletons

Acceptance:
- [ ] Page loads and shows 4 panels with loading states
- [ ] No JavaScript errors in console
- [ ] Layout doesn't break at 1280px

---

### S4.3 — SSE handler with context cancellation
**Commit:** `feat(S4.3): SSE endpoints with proper goroutine lifecycle`

Tasks:
- Create `internal/server/sse.go`:
  ```go
  type SSEWriter struct {
      w       http.ResponseWriter
      flusher http.Flusher
      ctx     context.Context
  }
  func NewSSEWriter(w http.ResponseWriter, r *http.Request) (*SSEWriter, error)
  func (s *SSEWriter) SendEvent(event, data string) error
  func (s *SSEWriter) SendHTML(event, html string) error
  ```
- Create SSE handlers in `handlers.go`:
  - `GET /stream/{sessionID}/triage` → runs Phase 1, sends triage HTML partial
  - `GET /stream/{sessionID}/timeline` → runs Phase 2, sends timeline HTML partial
  - `GET /stream/{sessionID}/rca` → runs Phase 3, streams markdown chunks as HTML
- Each handler:
  - Looks up session (404 if not found)
  - Checks cache first
  - Runs analysis with `r.Context()` so it cancels on disconnect
  - Sends `event: done` when complete
  - Uses `select { case <-ctx.Done(): return }` pattern

Acceptance:
- [ ] Close browser tab mid-stream → goroutine exits (verify with logging)
- [ ] SSE events appear in browser Network tab as stream
- [ ] Invalid session ID returns 404

---

### S4.4 — Risk gauge + timeline visual + RCA markdown rendering
**Commit:** `feat(S4.4): risk score gauge, timeline cards, and RCA markdown`

Tasks:
- Risk Scorecard partial:
  - Severity score as a colored number (red > 70, amber 40-70, green < 40)
  - List of top issues with severity badges (color-coded)
  - Cluster health tag
- Timeline partial:
  - Vertical timeline with dots (color by severity)
  - Each event: relative time, title, detail text
  - "T+0:00" as starting anchor
- RCA partial:
  - Render streaming markdown: use simple JS to convert `##` headers and `\`\`\`` code blocks
  - Code blocks (kubectl commands) have a copy-to-clipboard button
  - Preserve streaming — append chunks as they arrive, don't re-render whole panel
- Use Tailwind for all styling — no custom CSS

Acceptance:
- [ ] Score 85 renders red, score 45 renders amber, score 20 renders green
- [ ] Timeline shows events in order with correct colors
- [ ] kubectl commands in RCA have working copy buttons

---

## Epic 5 — Chat Interface
*Goal: Ask your bundle anything. Grounded, streaming, helpful.*

### S5.1 — Chat handler with conversation history
**Commit:** `feat(S5.1): chat handler with per-session conversation history`

Tasks:
- Add to session: `ChatHistory []analysis.ChatMessage`
- `analysis.ChatMessage`: `Role string`, `Content string`
- Create `POST /chat/{sessionID}` handler:
  - Reads `message` from form body
  - Appends to session's ChatHistory
  - Calls `RunChat(ctx, client, bundleData, history) (string, error)`
  - Appends assistant response to history
  - Returns rendered chat message partial
- `RunChat` builds messages array: system prompt with bundle context + history + new message
- System prompt = concise BundleData JSON + "You are a K8s expert analyzing this specific bundle. Only reference data from the bundle. Say 'I don't see that in the bundle' if asked about something not present."

Acceptance:
- [ ] Send a message → get response referencing actual bundle data (in stub mode)
- [ ] Second message has access to first message context
- [ ] History is stored in session (survives SSE stream completing)

---

### S5.2 — Grounded context injection
**Commit:** `feat(S5.2): keyword-based context retrieval for chat grounding`

Tasks:
- Add `FindRelevantContext(query string, data *bundle.BundleData) string`:
  - Tokenize query into keywords
  - Search pod names, event messages, log lines for keyword matches
  - Return top 5 most relevant excerpts (pod status + log lines + events)
  - Max 2000 chars of retrieved context
- Inject retrieved context into the chat message: "Relevant bundle data:\n{context}\n\nUser question: {message}"
- This prevents hallucination when history grows long

Acceptance:
- [ ] Query "nginx" returns context about nginx pods specifically
- [ ] Retrieved context is under 2000 chars
- [ ] Chat still works if no relevant context found (returns all pod summaries)

---

### S5.3 — Streaming chat UI
**Commit:** `feat(S5.3): streaming chat responses with HTMX`

Tasks:
- Update chat handler to stream via SSE instead of returning full response:
  - `GET /chat/{sessionID}/stream?message={encoded}` endpoint
  - Streams response tokens as they arrive
  - On `event: done`, sends the complete message for history storage
- Update `templates/partials/chat.html`:
  - Message bubbles (user = right-aligned gray, assistant = left-aligned white)
  - Typing indicator (animated dots) while streaming
  - Auto-scroll to bottom on new message
  - Input disabled while response streaming
  - HTMX SSE connection per message

Acceptance:
- [ ] Response streams in token by token
- [ ] Typing indicator shows while waiting
- [ ] Input re-enables after response completes

---

### S5.4 — Suggested starter questions
**Commit:** `feat(S5.4): context-aware suggested questions based on triage`

Tasks:
- After triage completes, generate 4 suggested questions based on top issues:
  - Template: if issue is OOMKilled → suggest "Why is {pod} getting OOMKilled?"
  - Template: if issue is CrashLoopBackOff → suggest "What is causing {pod} to crash?"
  - Template: if issue is ImagePullBackOff → suggest "How do I fix the image pull error on {pod}?"
  - Always include: "What should I fix first?" and "Give me all the kubectl commands to remediate this"
- Render as clickable pills below the chat input
- Clicking a pill populates the input and submits

Acceptance:
- [ ] 4 suggested questions appear after triage loads
- [ ] Clicking a question sends it as a chat message
- [ ] Questions reference actual pod names from the bundle

---

## Epic 6 — Test Bundle, Cache & Polish
*Goal: A great demo bundle, reliable experience, and a repo worth showing.*

### S6.1 — kind cluster setup script with scripted failures
**Commit:** `docs(S6.1): kind cluster setup script with multi-failure scenario`

Tasks:
- Create `scripts/setup-cluster.sh`:
  - Creates a kind cluster named `bundleiq-demo`
  - Deploys a simple "broken" application with these specific failures:
    1. **OOMKilled app**: a deployment with memory limit 32Mi running a memory-hungry container
    2. **CrashLoopBackOff**: a pod with a bad entrypoint command
    3. **ImagePullBackOff**: a deployment referencing `nginx:doesnotexist-v999`
    4. **Pending pod**: resource request > node capacity (request 32Gi memory)
    5. **Missing ConfigMap**: a deployment with `envFrom` referencing a ConfigMap that doesn't exist
  - Waits 60 seconds for failure states to stabilize
  - Prints pod statuses
- Create `scripts/generate-bundle.sh`:
  - Installs `kubectl support-bundle` plugin if not present
  - Runs against the kind cluster
  - Outputs `./demo-bundle-TIMESTAMP.tar.gz`
- Add `make demo-cluster` and `make demo-bundle` to Makefile

Acceptance:
- [ ] Script runs end-to-end without manual intervention
- [ ] Generated bundle is under 50MB
- [ ] `kubectl get pods -A` shows at least 3 non-Running pods

---

### S6.2 — Validate BundleIQ against demo bundle
**Commit:** `feat(S6.2): validate full pipeline against real demo bundle`

Tasks:
- Run BundleIQ against the generated demo bundle
- Fix any parser errors that show up (missing paths, unexpected JSON structure)
- Ensure all 5 failure modes appear in the triage output
- Add the demo bundle sha256 to `README.md` (for reproducibility reference)
- Document any bundle structure quirks discovered in `internal/bundle/NOTES.md`

Acceptance:
- [ ] Upload demo bundle → triage shows severity > 70
- [ ] All 5 failure modes mentioned somewhere in triage or RCA
- [ ] No panic or 500 errors in server logs

---

### S6.3 — Analysis result cache validation
**Commit:** `feat(S6.3): validate cache hit/miss behavior`

Tasks:
- Add cache hit logging: "Cache hit for bundle SHA256: {hash[:8]}"
- Add cache miss logging: "Cache miss, running analysis for SHA256: {hash[:8]}"
- Add `GET /debug/cache` endpoint (dev mode only) showing cache keys and ages
- Upload same bundle twice, verify second is instant and shows "cached" badge in UI
- Upload different bundle, verify new analysis runs

Acceptance:
- [ ] Second upload of same file completes in < 200ms
- [ ] UI shows "Cached result" badge on cached analyses
- [ ] Debug endpoint shows cache state

---

### S6.4 — README polish, approach doc, and demo prep
**Commit:** `docs(S6.4): complete README, approach doc, and demo script`

Tasks:
- Complete `README.md`:
  - Architecture diagram (ASCII or linked image)
  - Prerequisites: Go 1.22+, `kind`, `kubectl`, `air`
  - Quickstart (5 steps from clone to running analysis)
  - Environment variables table
  - How to generate your own demo bundle
  - Project structure explanation
- Complete `MY_APPROACH_AND_THOUGHTS.md` (max 500 words):
  - Why hierarchical summarization
  - The token budget problem and how we solved it
  - Interesting observations about the problem domain
  - What we'd do next with more time
- Create `scripts/demo-script.md`: a 2-minute walkthrough script for video recording

Acceptance:
- [ ] `README.md` < 400 lines, clear and scannable
- [ ] `MY_APPROACH_AND_THOUGHTS.md` exactly 500 words or under
- [ ] Demo script includes specific things to say and show at each moment

---

## Epic 7 — Dockerization (Final Deliverable)
*Goal: `docker-compose up` is the only command a reviewer needs to run Autopsy.*

### S7.1 — Embed templates + healthz endpoint
**Commit:** `feat(S7.1): embed templates with go:embed and add healthz endpoint`

Tasks:
- Add `//go:embed templates` directive in `main.go` or a dedicated `embed.go` file:
  ```go
  import "embed"

  //go:embed templates
  var templateFS embed.FS
  ```
- Update template parsing to use `templateFS` instead of reading from disk:
  ```go
  tmpl := template.Must(template.ParseFS(templateFS, "templates/*.html", "templates/partials/*.html"))
  ```
- Verify: `go build ./...` still works and templates render correctly in dev
- Add `GET /healthz` handler in `server/handlers.go`:
  ```go
  func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type", "application/json")
      w.WriteHeader(http.StatusOK)
      w.Write([]byte(`{"status":"ok","service":"autopsy"}`))
  }
  ```
- Register route: `mux.HandleFunc("GET /healthz", h.HandleHealthz)`
- This is required BEFORE writing the Dockerfile — the container health check depends on it

Acceptance:
- [ ] `curl localhost:8080/healthz` returns `{"status":"ok","service":"autopsy"}`
- [ ] App still works exactly as before (templates render, analysis runs)
- [ ] `go test -race ./...` passes
- [ ] No template files need to exist on disk at runtime — binary is self-contained

---

### S7.2 — Dockerfile (multi-stage) + .dockerignore
**Commit:** `feat(S7.2): multi-stage Dockerfile with non-root user`

Tasks:
- Create `Dockerfile` at repo root:

```dockerfile
# Stage 1: Builder
FROM golang:1.22-alpine AS builder

# Install git for go modules that need it
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary (CGO disabled for alpine compatibility)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o autopsy \
    .

# Stage 2: Minimal runtime
FROM alpine:3.19

# Security: non-root user
RUN addgroup -g 1001 autopsy && \
    adduser -D -u 1001 -G autopsy autopsy

# ca-certificates needed for HTTPS calls to Anthropic API
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy only the binary (templates are embedded)
COPY --from=builder /build/autopsy .

# Own everything as non-root user
RUN chown -R autopsy:autopsy /app

USER autopsy

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["./autopsy"]
```

- Create `.dockerignore`:
```
.git
.gitignore
.air.toml
tmp/
*.tar.gz
*.tgz
.env
.env.*
!.env.example
scripts/
README.md
MY_APPROACH_AND_THOUGHTS.md
```

- Add `make docker-build` to Makefile:
```makefile
## docker-build: Build the Docker image
docker-build:
	docker build -t autopsy:latest .
	@echo "Image size:"
	@docker image inspect autopsy:latest --format='{{.Size}}' | awk '{printf "%.1f MB\n", $$1/1024/1024}'
```

Acceptance:
- [ ] `docker build -t autopsy:latest .` succeeds with zero errors
- [ ] `docker image inspect autopsy:latest` shows image under 30MB
- [ ] `docker run --rm -e ANTHROPIC_API_KEY="" -p 8080:8080 autopsy:latest` starts in stub mode
- [ ] `curl localhost:8080/healthz` returns 200 from running container
- [ ] Container runs as uid 1001 (verify with `docker exec <id> whoami`)

---

### S7.3 — docker-compose.yml + make docker-run
**Commit:** `feat(S7.3): docker-compose for one-command startup`

Tasks:
- Create `docker-compose.yml` at repo root:

```yaml
services:
  autopsy:
    build:
      context: .
      dockerfile: Dockerfile
    image: autopsy:latest
    container_name: autopsy
    ports:
      - "8080:8080"
    environment:
      # Pass through from host environment
      # Set this in your shell: export ANTHROPIC_API_KEY=sk-ant-...
      # Or create a .env file (never commit it)
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}
      - PORT=8080
      - MAX_BUNDLE_MB=250
      - STUB_MODE=${STUB_MODE:-false}
      - SESSION_TTL_MINUTES=30
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

- Add Makefile targets:
```makefile
## docker-run: Run Autopsy via docker-compose (builds if needed)
docker-run:
	docker-compose up --build

## docker-stop: Stop the running container
docker-stop:
	docker-compose down

## docker-logs: Tail container logs
docker-logs:
	docker-compose logs -f autopsy
```

- Update `README.md` quickstart section to lead with Docker:
  ```
  ## Quickstart (Docker — recommended)
  export ANTHROPIC_API_KEY=sk-ant-...
  docker-compose up
  # Open http://localhost:8080
  
  ## Quickstart (local Go dev)
  cp .env.example .env  # add your API key
  make dev
  ```

Acceptance:
- [ ] `docker-compose up` builds and starts the container with zero manual steps
- [ ] App is reachable at `http://localhost:8080` after `docker-compose up`
- [ ] App runs in stub mode if `ANTHROPIC_API_KEY` is not set (warning in logs, yellow banner in UI)
- [ ] App runs live analysis if `ANTHROPIC_API_KEY` is a valid key
- [ ] `docker-compose down` stops the container cleanly
- [ ] `make docker-run` works as an alias

---

### S7.4 — Final validation + tag v1.0.0
**Commit:** `docs(S7.4): final README polish and v1.0.0 tag`

Tasks:
- Complete `README.md`:
  - Architecture diagram (ASCII)
  - Prerequisites table: Docker only for Docker path; Go 1.22+, kind, kubectl, air for dev path
  - Quickstart section: Docker first, then local dev
  - Environment variables table with descriptions and defaults
  - "How it works" section: 4-phase analysis pipeline explained in plain language
  - How to generate your own demo bundle (`make demo-cluster && make demo-bundle`)
  - Project structure tree
- Complete `MY_APPROACH_AND_THOUGHTS.md` (max 500 words):
  - Why hierarchical summarization over naive full-context dumping
  - The token budget problem and the EnforceBudget strategy
  - Why SSE + HTMX over WebSockets or React
  - What's next: persistent storage, multi-bundle comparison, ISV-specific analyzer configs
- Create `scripts/demo-script.md`: 2-minute video walkthrough script
- Final end-to-end Docker validation:
  ```bash
  docker-compose down
  docker-compose up --build
  # Upload demo bundle
  # Verify triage, timeline, RCA all appear
  # Send a chat message
  # Verify response references actual bundle data
  ```
- Apply version tag:
  ```bash
  git tag -a v1.0.0 -m "Autopsy v1.0 — AI-powered Kubernetes support bundle analyzer"
  git log --oneline  # Verify clean commit history
  ```

Acceptance:
- [ ] `docker-compose up` → full analysis pipeline works end-to-end with a real bundle
- [ ] `README.md` is clear enough that a stranger can run it in under 5 minutes
- [ ] `MY_APPROACH_AND_THOUGHTS.md` is under 500 words
- [ ] `git log --oneline` shows one commit per story, clean history
- [ ] `git tag` shows `v1.0.0`

---

## Commit Convention Summary

```
chore(S0.1): initialize go module and air live-reload
feat(S1.2): streaming tar.gz extraction to temp dir
fix(S2.3): handle missing log directory gracefully
docs(S0.3): README skeleton and cursor rules acknowledgment
feat(S7.2): multi-stage Dockerfile with non-root user
feat(S7.3): docker-compose for one-command startup
docs(S7.4): final README polish and v1.0.0 tag
```

**Rules:**
- One story = one commit, no exceptions
- All tests must pass before commit: `go test -race ./...`
- `go vet ./...` must be clean
- No `TODO` comments left in committed code (open a story instead)
- The very last action in the entire project is `git tag -a v1.0.0`

