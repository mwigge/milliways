#!/usr/bin/env python3
from __future__ import annotations

from html import escape
from pathlib import Path


OUT = Path(__file__).resolve().parent / "images"


def write(path: str, body: str) -> None:
    (OUT / path).write_text(body, encoding="utf-8")


def svg(width: int, height: int, content: str) -> str:
    return f"""<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}">
  <rect width="{width}" height="{height}" fill="#111827"/>
  <style>
    .title {{ font: 700 20px ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; fill: #f9fafb; }}
    .label {{ font: 600 14px ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; fill: #e5e7eb; }}
    .text {{ font: 13px ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; fill: #cbd5e1; }}
    .mono {{ font: 13px ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace; fill: #d1d5db; }}
    .muted {{ fill: #94a3b8; }}
    .ok {{ fill: #34d399; }}
    .warn {{ fill: #fbbf24; }}
    .accent {{ fill: #8b5cf6; }}
    .cyan {{ fill: #38bdf8; }}
    .pearl {{ fill: #f5f5f4; }}
    .box {{ fill: #1f2937; stroke: #475569; stroke-width: 1.4; rx: 10; }}
    .box2 {{ fill: #172033; stroke: #64748b; stroke-width: 1.4; rx: 10; }}
    .store {{ fill: #0f172a; stroke: #38bdf8; stroke-width: 1.4; rx: 10; }}
    .line {{ stroke: #94a3b8; stroke-width: 2; fill: none; marker-end: url(#arrow); }}
    .line2 {{ stroke: #8b5cf6; stroke-width: 2; fill: none; marker-end: url(#arrow2); }}
  </style>
  <defs>
    <marker id="arrow" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto" markerUnits="strokeWidth">
      <path d="M0,0 L0,6 L7,3 z" fill="#94a3b8"/>
    </marker>
    <marker id="arrow2" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto" markerUnits="strokeWidth">
      <path d="M0,0 L0,6 L7,3 z" fill="#8b5cf6"/>
    </marker>
  </defs>
{content}
</svg>
"""


def terminal(name: str, title: str, lines: list[tuple[str, str]], width: int = 1040) -> None:
    row_h = 22
    height = 74 + len(lines) * row_h
    out = [
        f'<rect x="24" y="22" width="{width - 48}" height="{height - 44}" rx="14" fill="#020617" stroke="#334155"/>',
        '<circle cx="52" cy="48" r="6" fill="#ef4444"/><circle cx="74" cy="48" r="6" fill="#f59e0b"/><circle cx="96" cy="48" r="6" fill="#22c55e"/>',
        f'<text x="124" y="53" class="mono muted">{escape(title)}</text>',
    ]
    y = 88
    for cls, text in lines:
        out.append(f'<text x="48" y="{y}" class="mono {cls}">{escape(text)}</text>')
        y += row_h
    write(name, svg(width, height, "\n".join(out)))


def box(x: int, y: int, w: int, h: int, title: str, lines: list[str], cls: str = "box") -> str:
    parts = [f'<rect x="{x}" y="{y}" width="{w}" height="{h}" class="{cls}"/>']
    parts.append(f'<text x="{x + 18}" y="{y + 28}" class="label">{escape(title)}</text>')
    yy = y + 54
    for line in lines:
        parts.append(f'<text x="{x + 18}" y="{yy}" class="text">{escape(line)}</text>')
        yy += 22
    return "\n".join(parts)


def line(x1: int, y1: int, x2: int, y2: int, cls: str = "line") -> str:
    return f'<path d="M{x1},{y1} C{(x1+x2)//2},{y1} {(x1+x2)//2},{y2} {x2},{y2}" class="{cls}"/>'


terminal(
    "milliways-evidence-linux-smoke.svg",
    "terminal-ui-smoke: Linux package smoke evidence",
    [
        ("pearl", "milliways version terminal-ui-smoke"),
        ("muted", ""),
        ("label", "Native package install"),
        ("ok", "  [ok] Ubuntu binary install"),
        ("ok", "  [ok] Fedora binary install"),
        ("ok", "  [ok] Arch binary install"),
        ("muted", ""),
        ("label", "Binary and daemon checks"),
        ("ok", "  [ok] milliways binary: /tmp/mw-install/bin/milliways"),
        ("ok", "  [ok] milliwaysd binary: /tmp/mw-install/bin/milliwaysd"),
        ("ok", "  [ok] milliwaysctl binary: /tmp/mw-install/bin/milliwaysctl"),
        ("ok", "  [ok] version reported: milliways version terminal-ui-smoke"),
        ("muted", ""),
        ("label", "4. Metrics and observability"),
        ("ok", "  [ok] metrics SQLite DB created"),
        ("ok", "  [ok] structured OTel logging active"),
        ("muted", ""),
        ("label", "5. Feature dependencies"),
        ("ok", "  [ok] MemPalace importable from feature Python"),
        ("ok", "  [ok] python-pptx importable from feature Python"),
        ("ok", "  [ok] CodeGraph command available"),
        ("muted", ""),
        ("label", "Local server and takeover"),
        ("ok", "  [ok] Ubuntu local server plus two CLI takeover smoke"),
        ("ok", "  [ok] Fedora local server plus two CLI takeover smoke"),
        ("ok", "  [ok] Arch local server plus two CLI takeover smoke"),
        ("muted", ""),
        ("warn", "  [skip] amd64 source fallback on arm64 Docker host"),
    ],
)

terminal(
    "milliways-observability-cockpit.svg",
    "MilliWays.app lower-left observability cockpit",
    [
        ("accent", "milliways observability -- 10:42:18Z"),
        ("pearl", "latest: rpc:status.get             0.49ms  ok"),
        ("muted", ""),
        ("label", "summary"),
        ("mono", "  total spans:   18"),
        ("mono", "  error rate:    0/min"),
        ("mono", "  p50 latency:   0.50ms"),
        ("mono", "  p99 latency:   0.92ms"),
        ("muted", ""),
        ("label", "usage"),
        ("mono", "  tokens:        in 1.2k / out 340 / total 1.5k"),
        ("mono", "  cost:          $0.0041"),
        ("mono", "  time to limit: hosted-runner 6.4h"),
        ("muted", ""),
        ("cyan", "latency: p50 [====] p95 [=======] p99 [========]"),
    ],
    width=980,
)

write(
    "milliways-memory-session-flow.svg",
    svg(
        1120,
        520,
        "\n".join(
            [
                '<text x="40" y="52" class="title">Session memory flow</text>',
                box(40, 92, 210, 120, "Prompt", ["user input", "slash commands", "context fragments"]),
                box(310, 72, 230, 160, "Context builder", ["active turn log", "project facts", "repo context"]),
                box(610, 50, 220, 120, "Project memory", ["MemPalace MCP", "relevant drawers", "handoff facts"], "store"),
                box(610, 206, 220, 120, "Code context", ["CodeGraph index", "symbols", "call graph"], "store"),
                box(890, 92, 190, 120, "Runner", ["streaming output", "tool events", "usage data"]),
                box(310, 310, 230, 120, "Live turn log", ["in-memory", "used for switch", "compactable"], "box2"),
                box(610, 352, 220, 120, "Daemon history", ["history/*.ndjson", "bounded retention", "per runner"], "store"),
                line(250, 152, 310, 152),
                line(540, 132, 610, 110),
                line(540, 172, 610, 254),
                line(830, 110, 890, 152),
                line(830, 254, 890, 152),
                line(980, 212, 720, 352),
                line(890, 174, 540, 370),
                line(425, 310, 425, 232),
            ]
        ),
    ),
)

write(
    "milliways-persistence-map.svg",
    svg(
        1120,
        520,
        "\n".join(
            [
                '<text x="40" y="52" class="title">Persistence map</text>',
                box(40, 90, 250, 130, "Live process", ["current REPL turn log", "stream state", "switch briefing"], "box2"),
                box(350, 70, 300, 120, "Config", ["~/.config/milliways/local.env", "login, model, path, endpoint"], "store"),
                box(730, 70, 310, 120, "Metrics", ["~/.local/state/milliways/metrics.db", "tokens, cost, latency, errors"], "store"),
                box(350, 235, 300, 120, "Event history", ["~/.local/state/milliways/history", "one ndjson file per runner"], "store"),
                box(730, 235, 310, 120, "Project memory", ["~/.mempalace", "facts, drawers, handoff records"], "store"),
                box(350, 400, 300, 80, "Code index", [".codegraph workspace", "symbol and dependency context"], "store"),
                line(290, 150, 350, 130),
                line(290, 150, 350, 295),
                line(290, 150, 730, 130),
                line(290, 150, 730, 295),
                line(500, 355, 500, 400),
                '<text x="58" y="254" class="text muted">Boundary: live turn log is not full automatic restore yet.</text>',
                '<text x="58" y="280" class="text muted">Persisted stores survive process restart and power loss.</text>',
            ]
        ),
    ),
)

write(
    "milliways-takeover-flow.svg",
    svg(
        1120,
        500,
        "\n".join(
            [
                '<text x="40" y="52" class="title">Takeover and handoff flow</text>',
                box(40, 110, 210, 120, "Active runner", ["current task", "recent turns", "last tool state"]),
                box(310, 95, 230, 150, "Briefing builder", ["summarise intent", "capture decisions", "carry next step"]),
                box(610, 70, 220, 120, "Same REPL switch", ["inject briefing", "continue immediately"], "box2"),
                box(610, 245, 220, 120, "Cross-pane handoff", ["write handoff fact", "target reads on open"], "store"),
                box(890, 150, 190, 120, "Target runner", ["project memory", "briefing", "new prompt"]),
                line(250, 170, 310, 170),
                line(540, 145, 610, 130, "line2"),
                line(540, 195, 610, 305),
                line(830, 130, 890, 190, "line2"),
                line(830, 305, 890, 210),
                '<text x="314" y="286" class="text muted">If MemPalace is unavailable, cross-pane takeover degrades to local context only.</text>',
            ]
        ),
    ),
)
