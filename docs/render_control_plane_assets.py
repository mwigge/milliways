#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path
from textwrap import wrap

from PIL import Image, ImageDraw, ImageFont


OUT = Path(__file__).resolve().parent / "images"
W, H = 1600, 900

BG = "#0b1220"
PANEL = "#111c2f"
PANEL_2 = "#15253d"
INK = "#f7fafc"
MUTED = "#9fb0c7"
LINE = "#5f7394"
GREEN = "#38d47a"
AMBER = "#f2b84b"
RED = "#f87171"
CYAN = "#40c7ff"
BLUE = "#7aa2ff"
PURPLE = "#b28cff"


def font(size: int, bold: bool = False) -> ImageFont.FreeTypeFont:
    names = [
        "/usr/share/fonts/TTF/DejaVuSans-Bold.ttf" if bold else "/usr/share/fonts/TTF/DejaVuSans.ttf",
        "/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf" if bold else "/usr/share/fonts/dejavu/DejaVuSans.ttf",
    ]
    for name in names:
        path = Path(name)
        if path.exists():
            return ImageFont.truetype(str(path), size)
    return ImageFont.load_default()


F_TITLE = font(46, True)
F_SUB = font(25)
F_LABEL = font(25, True)
F_TEXT = font(22)
F_SMALL = font(19)
F_MONO = font(20)


def canvas(title: str, subtitle: str = "") -> tuple[Image.Image, ImageDraw.ImageDraw]:
    img = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(img)
    d.rectangle((0, 0, W, 116), fill="#07111f")
    d.text((64, 38), title, fill=INK, font=F_TITLE)
    if subtitle:
        d.text((66, 96), subtitle, fill=MUTED, font=F_SUB)
    return img, d


def text_block(d: ImageDraw.ImageDraw, xy: tuple[int, int], text: str, fill: str = MUTED, width: int = 36, line_h: int = 28) -> int:
    x, y = xy
    for paragraph in text.split("\n"):
        for line in wrap(paragraph, width=width):
            d.text((x, y), line, fill=fill, font=F_TEXT)
            y += line_h
        y += 6
    return y


def box(d: ImageDraw.ImageDraw, xywh: tuple[int, int, int, int], title: str, body: str, accent: str = CYAN, fill: str = PANEL) -> None:
    x, y, w, h = xywh
    d.rounded_rectangle((x, y, x + w, y + h), radius=22, fill=fill, outline="#2a3b56", width=3)
    d.rectangle((x, y, x + 8, y + h), fill=accent)
    d.text((x + 28, y + 24), title, fill=INK, font=F_LABEL)
    text_block(d, (x + 28, y + 66), body, width=max(22, int(w / 15)))


def pill(d: ImageDraw.ImageDraw, xy: tuple[int, int], text: str, color: str) -> None:
    x, y = xy
    tw = d.textlength(text, font=F_SMALL)
    d.rounded_rectangle((x, y, x + int(tw) + 58, y + 38), radius=19, fill="#0d1729", outline=color, width=2)
    d.ellipse((x + 12, y + 13, x + 24, y + 25), fill=color)
    d.text((x + 32, y + 8), text, fill=INK, font=F_SMALL)


def arrow(d: ImageDraw.ImageDraw, start: tuple[int, int], end: tuple[int, int], color: str = LINE, width: int = 4) -> None:
    d.line((start, end), fill=color, width=width)
    x1, y1 = start
    x2, y2 = end
    if abs(x2 - x1) >= abs(y2 - y1):
        sign = 1 if x2 > x1 else -1
        pts = [(x2, y2), (x2 - sign * 20, y2 - 12), (x2 - sign * 20, y2 + 12)]
    else:
        sign = 1 if y2 > y1 else -1
        pts = [(x2, y2), (x2 - 12, y2 - sign * 20), (x2 + 12, y2 - sign * 20)]
    d.polygon(pts, fill=color)


def save(img: Image.Image, name: str) -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    img.save(OUT / name, "PNG", optimize=True)


def draw_control_plane() -> None:
    img, d = canvas("Secure MilliWays control plane", "One terminal-owned policy layer around many AI clients")
    box(d, (70, 190, 330, 470), "AI clients", "Claude\nCodex\nCopilot\nGemini\nPool\nMiniMax\nLocal models", GREEN)
    box(d, (500, 170, 600, 510), "MilliWays daemon", "Shared sessions, memory, client switching, policy decisions, audit events, scanner status, and observability snapshots.", CYAN, PANEL_2)
    box(d, (1200, 190, 330, 470), "Evidence stores", "metrics.db\nhistory/*.ndjson\nsecurity audit\nSBOM\nCRA evidence\nquarantine plans", PURPLE)
    arrow(d, (400, 420), (500, 420), GREEN)
    arrow(d, (1100, 420), (1200, 420), PURPLE)
    pill(d, (550, 720), "SEC OK / WARN / BLOCK", GREEN)
    pill(d, (835, 720), "protected / unprotected clients", CYAN)
    pill(d, (1210, 720), "auditable decisions", PURPLE)
    save(img, "secure-control-plane-overview.png")


def draw_client_protection() -> None:
    img, d = canvas("Client protection states", "The UI must say what MilliWays can actually enforce")
    rows = [
        ("MiniMax", "full", "MilliWays owns the request and tool execution path.", GREEN),
        ("Local", "full", "OpenAI-compatible local endpoint with MilliWays-owned tools.", GREEN),
        ("Claude", "brokered", "Protected when launched through controlled env and active shims.", CYAN),
        ("Codex", "brokered", "Protected when the command broker is active and auditable.", CYAN),
        ("Gemini / Pool / Copilot", "preflight-only", "Startup and client-profile checks when no broker path is active.", AMBER),
        ("Unknown CLI", "unprotected", "Visible as unprotected until it joins the control plane.", RED),
    ]
    y = 170
    for client, state, body, color in rows:
        d.rounded_rectangle((80, y, 1520, y + 88), radius=18, fill=PANEL, outline="#2a3b56", width=2)
        d.text((116, y + 26), client, fill=INK, font=F_LABEL)
        pill(d, (520, y + 25), state, color)
        d.text((850, y + 30), body, fill=MUTED, font=F_SMALL)
        y += 110
    save(img, "secure-control-plane-client-states.png")


def draw_command_path() -> None:
    img, d = canvas("Command path: deterministic gate before execution", "Package installs, persistence, secret reads, downloads, shell eval, exfiltration patterns, and IOC hits")
    box(d, (70, 230, 270, 210), "Agent command", "npm install\ncurl | sh\ncat .env\nsystemctl --user", GREEN)
    box(d, (430, 210, 310, 250), "Command firewall", "Parse argv or shell text. Classify risk without asking the model to judge itself.", CYAN, PANEL_2)
    box(d, (830, 165, 300, 150), "warn", "Record and continue. Useful for developer laptops.", AMBER)
    box(d, (830, 370, 300, 150), "block", "Stop known-bad or high-risk actions in strict/CI.", RED)
    box(d, (1230, 250, 280, 210), "Audit trail", "workspace\nclient\nsession\nargv\nreason\nmode", PURPLE)
    arrow(d, (340, 335), (430, 335), GREEN)
    arrow(d, (740, 300), (830, 240), AMBER)
    arrow(d, (740, 360), (830, 445), RED)
    arrow(d, (1130, 240), (1230, 315), PURPLE)
    arrow(d, (1130, 445), (1230, 395), PURPLE)
    save(img, "secure-control-plane-command-path.png")


def draw_observability() -> None:
    img, d = canvas("Security in the observability cockpit", "Risk state should be visible while the agent is working")
    d.rounded_rectangle((90, 160, 1510, 710), radius=28, fill="#07111f", outline="#2a3b56", width=3)
    d.text((130, 215), "milliways observability", fill=INK, font=F_LABEL)
    box(d, (130, 280, 385, 250), "Posture", "SEC WARN\nstartup scan current\n2 warnings\n0 blocks", AMBER)
    box(d, (570, 280, 385, 250), "Client", "codex protected\nbrokered command path\nshims ready\nprofile checked", GREEN)
    box(d, (1010, 280, 385, 250), "Evidence", "OSV ready\nGitleaks ready\nSBOM current\nCRA evidence 74%", CYAN)
    d.text((130, 610), "The point is not another dashboard. It is keeping the minimum useful security signal next to tokens, cost, latency, and runner state.", fill=MUTED, font=F_TEXT)
    save(img, "secure-control-plane-observability.png")


def draw_evidence_loop() -> None:
    img, d = canvas("Evidence loop: scan, decide, record, improve", "Security work becomes operational evidence instead of one-off terminal output")
    items = [
        ((95, 230, 250, 170), "Scan", "startup scan\nclient profile\nOSV / gitleaks / semgrep", CYAN),
        ((430, 230, 250, 170), "Decide", "warn / block\nstrict / CI\nconfirmation gate", AMBER),
        ((765, 230, 250, 170), "Record", "policy audit\nwarnings\nSBOM\nCRA checks", PURPLE),
        ((1100, 230, 250, 170), "Recover", "quarantine plan\nrule updates\nfix evidence gaps", GREEN),
    ]
    for rect, title, body, color in items:
        box(d, rect, title, body, color)
    arrow(d, (345, 315), (430, 315), LINE)
    arrow(d, (680, 315), (765, 315), LINE)
    arrow(d, (1015, 315), (1100, 315), LINE)
    arrow(d, (1225, 400), (225, 520), GREEN)
    d.text((160, 580), "The loop is intentionally boring: collect evidence, make a policy decision, keep the audit, then improve the workspace.", fill=INK, font=F_SUB)
    d.text((160, 625), "That is how AI client security becomes repeatable across Claude, Codex, Gemini, Pool, Copilot, MiniMax, and local models.", fill=MUTED, font=F_TEXT)
    save(img, "secure-control-plane-evidence-loop.png")


def main() -> None:
    draw_control_plane()
    draw_client_protection()
    draw_command_path()
    draw_observability()
    draw_evidence_loop()


if __name__ == "__main__":
    main()
