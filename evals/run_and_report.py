# Requirements: python-dotenv
# Install: pip install python-dotenv
# Usage:   python evals/run_and_report.py

from __future__ import annotations

import re
import sys
import subprocess
from datetime import datetime
from pathlib import Path

PHASES        = ["Triage", "Timeline", "RCA", "Chat"]
MODEL_DISPLAY = "claude-sonnet-4-6"

# ── env ───────────────────────────────────────────────────────────────────────

def load_env(repo_root: Path) -> None:
    try:
        from dotenv import load_dotenv
    except ImportError:
        print("Warning: python-dotenv not installed — run: pip install python-dotenv",
              file=sys.stderr)
        return
    env_file = repo_root / ".env"
    if env_file.exists():
        load_dotenv(env_file)


# ── runner ────────────────────────────────────────────────────────────────────

def run_evals(repo_root: Path) -> tuple[str, int]:
    proc = subprocess.Popen(
        ["go", "test", "-v", "-tags=evals", "-timeout=5m", "./evals/..."],
        cwd=repo_root,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    lines: list[str] = []
    for line in proc.stdout:
        print(line, end="", flush=True)
        lines.append(line)
    proc.wait()
    return "".join(lines), proc.returncode


# ── name cleanup ──────────────────────────────────────────────────────────────

def clean_name(raw: str) -> tuple[str, str]:
    """
    Returns (phase, display).
      TestEval_Triage_ToyotaBundle_SeverityIsHigh  → ("Triage",   "Triage › SeverityIsHigh")
      TestEval_Chat_OOMQuestion_MentionsMemoryLimit → ("Chat",     "Chat › OOMQuestion_MentionsMemoryLimit")
    """
    stem  = raw.removeprefix("TestEval_")
    parts = stem.split("_", 2)
    phase = parts[0] if parts else "Other"
    if len(parts) >= 3 and parts[1] == "ToyotaBundle":
        detail = parts[2]
    elif len(parts) >= 2:
        detail = "_".join(parts[1:])
    else:
        detail = stem
    return phase, f"{phase} \u203a {detail}"


# ── parser ────────────────────────────────────────────────────────────────────

_RUN      = re.compile(r"^=== RUN\s+(\S+)")
_RESULT   = re.compile(r"^\s*--- (PASS|FAIL|SKIP):\s+(\S+)\s+\((\d+\.\d+)s\)")
_BUNDLE   = re.compile(
    r"bundle parsed pods=(\d+) nodes=(\d+) events=\d+ logExcerpts=\d+ "
    r"parseErrors=(\d+) tokenEstimate=(\d+)"
)
_EVAL     = re.compile(r"\[eval\]\s+(.+?):\s*(.*)")
_FILELINE = re.compile(r"^\s+\S+\.go:\d+:\s+(.*)")
_TIMEOUT  = re.compile(r"panic: test timed out")


def parse_output(raw: str) -> list[dict]:
    tests: list[dict]  = []
    cur:   dict | None = None
    timed_out_global   = False

    lines = raw.splitlines()
    n     = len(lines)
    i     = 0

    while i < n:
        line = lines[i]

        if _TIMEOUT.search(line):
            timed_out_global = True
            if cur:
                cur["timed_out"] = True

        # === RUN — only track top-level tests (no "/" in name)
        m = _RUN.match(line)
        if m:
            name = m.group(1)
            if "/" not in name:
                cur = {
                    "raw":      name,
                    "status":   None,
                    "duration": 0.0,
                    "input":    None,   # "pods=N nodes=N parseErrors=N tokens=~Xk"
                    "evals":    [],     # ["Label: value", ...]
                    "body":     [],     # raw lines for failure extraction only
                    "timed_out": False,
                }
            elif cur:
                cur["body"].append(line)
            i += 1
            continue

        # --- PASS / FAIL / SKIP
        m = _RESULT.match(line)
        if m:
            status, name, dur = m.group(1), m.group(2), float(m.group(3))
            if "/" not in name:
                if cur and cur["raw"] == name:
                    cur["status"]   = "TIMEOUT" if (cur["timed_out"] or timed_out_global) else status
                    cur["duration"] = dur
                    tests.append(cur)
                    cur = None
            elif cur:
                cur["body"].append(line)
            i += 1
            continue

        if cur is None:
            i += 1
            continue

        cur["body"].append(line)

        # Input: slog line emitted by bundle.Parse
        bm = _BUNDLE.search(line)
        if bm and cur["input"] is None:
            pods   = int(bm.group(1))
            nodes  = int(bm.group(2))
            errors = int(bm.group(3))
            tokens = int(bm.group(4))
            tok_s  = f"~{tokens // 1000}k" if tokens >= 1000 else str(tokens)
            cur["input"] = f"pods={pods} nodes={nodes} parseErrors={errors} tokens={tok_s}"

        # [eval] output lines — format: "[eval] Label:\nvalue" (value on next line)
        em = _EVAL.search(line)
        if em:
            label = em.group(1).strip()
            value = em.group(2).strip()
            # Value may span multiple tab-indented continuation lines
            if not value:
                j = i + 1
                val_parts: list[str] = []
                while j < n:
                    nxt = lines[j]
                    if nxt and (nxt[0] == "\t" or nxt[:4] == "    "):
                        val_parts.append(nxt.strip())
                        j += 1
                    else:
                        break
                if val_parts:
                    value = " ".join(val_parts)
            if value:
                value = (value[:120] + "\u2026") if len(value) > 120 else value
                cur["evals"].append(f"{label}: {value}")
            else:
                cur["evals"].append(f"{label}:")

        i += 1

    # Post-process: attach display name + failure reason
    for t in tests:
        t["phase"], t["display"] = clean_name(t["raw"])
        t["fail_reason"] = _failure_reason(t) if t["status"] == "FAIL" else ""

    return tests


def _failure_reason(test: dict) -> str:
    """First t.Errorf / t.Fatalf message after the last [eval] line, ≤200 chars."""
    body = test["body"]

    last_eval = -1
    for idx, line in enumerate(body):
        if "[eval]" in line:
            last_eval = idx

    noise = ("[", "INFO ", "WARN ", "DEBUG ")
    for line in body[last_eval + 1:]:
        m = _FILELINE.match(line)
        if m:
            msg = m.group(1).strip()
            if msg and not any(msg.startswith(p) for p in noise):
                return (msg[:200] + "\u2026") if len(msg) > 200 else msg
    return ""


# ── HTML helpers ──────────────────────────────────────────────────────────────

def _e(s: str) -> str:
    return (s
            .replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
            .replace('"', "&quot;"))


# ── HTML report ───────────────────────────────────────────────────────────────

def generate_html(
    tests: list[dict],
    timestamp_human: str,
    raw_path: str,
    wall_time: float,
) -> str:
    total    = len(tests)
    passed   = sum(1 for t in tests if t["status"] == "PASS")
    failed   = sum(1 for t in tests if t["status"] == "FAIL")
    skipped  = sum(1 for t in tests if t["status"] == "SKIP")
    timedout = sum(1 for t in tests if t["status"] == "TIMEOUT")
    rate     = round(passed / total * 100) if total else 0
    bar_col  = "#22c55e" if rate >= 80 else ("#f59e0b" if rate >= 50 else "#ef4444")

    # Group tests by phase, preserving run order within each
    by_phase: dict[str, list[dict]] = {p: [] for p in PHASES}
    for t in tests:
        bucket = t["phase"] if t["phase"] in by_phase else PHASES[-1]
        by_phase[bucket].append(t)

    # Build one <details> section per phase
    sections: list[str] = []
    for phase in PHASES:
        pts = by_phase[phase]
        if not pts:
            continue

        pp = sum(1 for t in pts if t["status"] == "PASS")
        pf = sum(1 for t in pts if t["status"] == "FAIL")
        pt = sum(1 for t in pts if t["status"] == "TIMEOUT")

        pills: list[str] = []
        if pp:
            pills.append(f'<span class="mpill mp-pass">{pp} passed</span>')
        if pf:
            pills.append(f'<span class="mpill mp-fail">{pf} failed</span>')
        if pt:
            pills.append(f'<span class="mpill mp-tout">{pt} timed&nbsp;out</span>')

        rows: list[str] = []
        for t in pts:
            st       = t["status"] or "UNKNOWN"
            bcls     = {"PASS": "bp", "FAIL": "bf", "SKIP": "bs", "TIMEOUT": "bt"}.get(st, "bs")
            name_h   = _e(t["display"])
            input_h  = _e(t["input"] or "\u2014")
            evals_h  = "<br>".join(_e(ev) for ev in t["evals"]) or "\u2014"
            dur_h    = f"{t['duration']:.2f}s"
            fail_h   = f'<span class="fail-text">{_e(t["fail_reason"])}</span>' if t["fail_reason"] else ""
            row_cls  = " rfail" if st == "FAIL" else (" rtout" if st == "TIMEOUT" else "")
            rows.append(
                f'<tr class="tr{row_cls}">'
                f'<td><span class="badge {bcls}">{st}</span></td>'
                f'<td class="cn">{name_h}</td>'
                f'<td class="ci">{input_h}</td>'
                f'<td class="co">{evals_h}</td>'
                f'<td class="cd">{dur_h}</td>'
                f'<td class="cf">{fail_h}</td>'
                f'</tr>'
            )

        sections.append(
            f'<details open class="sec">'
            f'<summary class="sec-hdr">'
            f'<span class="sec-caret">&#9654;</span>'
            f'<span class="sec-title">{phase}</span>'
            f'<span class="sec-pills">{"".join(pills)}</span>'
            f'</summary>'
            f'<table>'
            f'<thead><tr>'
            f'<th style="width:76px">Status</th>'
            f'<th>Test</th>'
            f'<th style="width:210px">Input</th>'
            f'<th>Output</th>'
            f'<th style="width:70px">Duration</th>'
            f'<th>Failure</th>'
            f'</tr></thead>'
            f'<tbody>{"".join(rows)}</tbody>'
            f'</table>'
            f'</details>'
        )

    sections_html = "\n".join(sections)

    # ── inline CSS + HTML ─────────────────────────────────────────────────────
    # All CSS/JS braces are doubled because this is inside an f-string.
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Autopsy — Eval Report</title>
<style>
*,*::before,*::after{{box-sizing:border-box;margin:0;padding:0}}
:root{{
  --bg:#0a0c10;--card:#13161f;--bdr:#1e2130;--bdr2:#252a3a;
  --tx:#e2e8f0;--dim:#94a3b8;--mut:#64748b;
  --grn:#22c55e;--red:#ef4444;--yel:#f59e0b;
  --mono:"SF Mono","Fira Code",ui-monospace,monospace;
}}
body{{background:var(--bg);color:var(--tx);font-family:system-ui,-apple-system,sans-serif;
  font-size:14px;line-height:1.5;padding:32px 28px;max-width:1300px;margin:0 auto}}

/* ── header ── */
.hdr{{display:flex;align-items:flex-start;justify-content:space-between;
  gap:16px;margin-bottom:28px;padding-bottom:20px;border-bottom:1px solid var(--bdr)}}
.hdr h1{{font-size:20px;font-weight:700;color:#f1f5f9;letter-spacing:-.3px}}
.hdr-meta{{text-align:right;font-family:var(--mono);font-size:11.5px;
  color:var(--mut);line-height:1.9}}

/* ── summary cards ── */
.stats{{display:flex;gap:10px;margin-bottom:12px;flex-wrap:wrap}}
.stat{{background:var(--card);border:1px solid var(--bdr);border-radius:10px;
  padding:14px 20px;flex:1;min-width:110px}}
.stat-lbl{{font-size:11px;font-weight:600;text-transform:uppercase;
  letter-spacing:.7px;color:var(--mut);margin-bottom:5px}}
.stat-val{{font-family:var(--mono);font-size:28px;font-weight:700;line-height:1}}
.sv-tot{{color:#f1f5f9}}.sv-pass{{color:var(--grn)}}.sv-fail{{color:var(--red)}}.sv-tout{{color:var(--yel)}}

/* ── progress bar ── */
.prog{{background:var(--card);border:1px solid var(--bdr);border-radius:10px;
  padding:14px 20px;margin-bottom:24px}}
.prog-lbl{{display:flex;justify-content:space-between;font-size:12px;
  color:var(--mut);margin-bottom:8px}}
.prog-lbl strong{{color:var(--tx)}}
.prog-track{{height:6px;background:var(--bdr2);border-radius:3px;overflow:hidden}}
.prog-fill{{height:100%;border-radius:3px}}

/* ── sections ── */
.sec{{background:var(--card);border:1px solid var(--bdr);border-radius:10px;
  margin-bottom:14px;overflow:hidden}}
.sec-hdr{{display:flex;align-items:center;gap:10px;padding:13px 18px;
  cursor:pointer;user-select:none;border-bottom:1px solid var(--bdr);list-style:none}}
.sec-hdr::-webkit-details-marker{{display:none}}
.sec-caret{{font-size:9px;color:var(--mut);transition:transform .15s;flex-shrink:0}}
details[open] .sec-caret{{transform:rotate(90deg)}}
.sec-title{{font-weight:600;font-size:15px;color:#f1f5f9;flex:1}}
.sec-pills{{display:flex;gap:6px;flex-wrap:wrap}}
.mpill{{font-size:11px;font-weight:600;padding:2px 8px;border-radius:20px;white-space:nowrap}}
.mp-pass{{background:rgba(34,197,94,.12);color:var(--grn);border:1px solid rgba(34,197,94,.28)}}
.mp-fail{{background:rgba(239,68,68,.12);color:var(--red);border:1px solid rgba(239,68,68,.28)}}
.mp-tout{{background:rgba(245,158,11,.12);color:var(--yel);border:1px solid rgba(245,158,11,.28)}}

/* ── table ── */
table{{width:100%;border-collapse:collapse}}
thead tr{{background:#0d0f16}}
thead th{{padding:8px 13px;text-align:left;font-size:11px;font-weight:600;
  text-transform:uppercase;letter-spacing:.7px;color:var(--mut);
  border-bottom:1px solid var(--bdr)}}
.tr{{border-bottom:1px solid var(--bdr2);transition:background .1s}}
.tr:last-child{{border-bottom:none}}
.tr:hover{{background:rgba(255,255,255,.025)}}
.rfail{{background:rgba(239,68,68,.04)}}.rfail:hover{{background:rgba(239,68,68,.08)}}
.rtout{{background:rgba(245,158,11,.04)}}.rtout:hover{{background:rgba(245,158,11,.08)}}
td{{padding:9px 13px;vertical-align:top}}

/* ── cell types ── */
.cn{{font-size:13px;color:var(--tx)}}
.ci{{font-family:var(--mono);font-size:11px;color:var(--mut);white-space:nowrap}}
.co{{font-size:12px;color:var(--dim);line-height:1.6}}
.cd{{font-family:var(--mono);font-size:11.5px;color:var(--mut);white-space:nowrap}}
.cf{{font-size:12px}}
.fail-text{{color:var(--red);font-style:italic}}

/* ── badges ── */
.badge{{display:inline-block;padding:2px 9px;border-radius:20px;
  font-family:var(--mono);font-size:10.5px;font-weight:700;
  letter-spacing:.3px;white-space:nowrap}}
.bp{{background:rgba(34,197,94,.13);color:var(--grn);border:1px solid rgba(34,197,94,.3)}}
.bf{{background:rgba(239,68,68,.13);color:var(--red);border:1px solid rgba(239,68,68,.3)}}
.bs{{background:rgba(100,116,139,.1);color:var(--mut);border:1px solid rgba(100,116,139,.25)}}
.bt{{background:rgba(245,158,11,.13);color:var(--yel);border:1px solid rgba(245,158,11,.3)}}

/* ── footer ── */
.ftr{{margin-top:24px;padding-top:16px;border-top:1px solid var(--bdr);
  font-family:var(--mono);font-size:11px;color:var(--mut);
  display:flex;flex-direction:column;gap:3px}}
</style>
</head>
<body>

<div class="hdr">
  <div><h1>Autopsy &mdash; Eval Report</h1></div>
  <div class="hdr-meta">
    <div>{_e(timestamp_human)}</div>
    <div>{_e(MODEL_DISPLAY)}</div>
  </div>
</div>

<div class="stats">
  <div class="stat"><div class="stat-lbl">Total</div><div class="stat-val sv-tot">{total}</div></div>
  <div class="stat"><div class="stat-lbl">Passed</div><div class="stat-val sv-pass">{passed}</div></div>
  <div class="stat"><div class="stat-lbl">Failed</div><div class="stat-val sv-fail">{failed}</div></div>
  <div class="stat"><div class="stat-lbl">Timed&nbsp;Out</div><div class="stat-val sv-tout">{timedout}</div></div>
</div>

<div class="prog">
  <div class="prog-lbl">
    <span>Pass rate</span>
    <strong>{rate}% &nbsp;({passed}/{total})</strong>
  </div>
  <div class="prog-track">
    <div class="prog-fill" style="width:{rate}%;background:{bar_col}"></div>
  </div>
</div>

{sections_html}

<div class="ftr">
  <span>Generated: {_e(timestamp_human)}</span>
  <span>Raw output: {_e(raw_path)}</span>
  <span>Wall time: {wall_time:.1f}s</span>
</div>

</body>
</html>"""


# ── main ──────────────────────────────────────────────────────────────────────

def main() -> None:
    script_dir = Path(__file__).parent.resolve()
    repo_root  = script_dir.parent

    load_env(repo_root)

    results_dir = script_dir / "results"
    results_dir.mkdir(exist_ok=True)

    now             = datetime.now()
    stamp           = now.strftime("%Y%m%dT%H%M%S")
    timestamp_human = now.strftime("%Y-%m-%d %H:%M:%S")

    t0  = datetime.now()
    raw, _rc = run_evals(repo_root)
    wall_time = (datetime.now() - t0).total_seconds()

    raw_path = results_dir / f"raw_{stamp}.txt"
    raw_path.write_text(raw, encoding="utf-8")

    tests = parse_output(raw)

    try:
        display_raw = str(raw_path.relative_to(repo_root))
    except ValueError:
        display_raw = raw_path.name
    html        = generate_html(tests, timestamp_human, display_raw, wall_time)
    report_path = results_dir / f"report_{stamp}.html"
    report_path.write_text(html, encoding="utf-8")

    print(f"Report written to {report_path}")

    sys.exit(1 if any(t["status"] == "FAIL" for t in tests) else 0)


if __name__ == "__main__":
    main()
