#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path
from textwrap import wrap

from PIL import Image, ImageDraw, ImageFont


OUT = Path(__file__).resolve().parent / "images"
W, H = 1600, 900

BG = "#0b1220"
HEADER = "#07111f"
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


F_TITLE = font(45, True)
F_SUB = font(24)
F_LABEL = font(25, True)
F_TEXT = font(22)
F_SMALL = font(19)
F_MONO = font(20)


def canvas(title: str, subtitle: str = "") -> tuple[Image.Image, ImageDraw.ImageDraw]:
    img = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(img)
    d.rectangle((0, 0, W, 118), fill=HEADER)
    d.text((64, 34), title, fill=INK, font=F_TITLE)
    if subtitle:
        d.text((66, 92), subtitle, fill=MUTED, font=F_SUB)
    return img, d


def save(img: Image.Image, name: str) -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    img.save(OUT / name, "PNG", optimize=True)


def text_block(d: ImageDraw.ImageDraw, xy: tuple[int, int], text: str, fill: str = MUTED, width: int = 42, line_h: int = 29) -> int:
    x, y = xy
    for paragraph in text.split("\n"):
        for line in wrap(paragraph, width=width):
            d.text((x, y), line, fill=fill, font=F_TEXT)
            y += line_h
        y += 6
    return y


def box(d: ImageDraw.ImageDraw, xywh: tuple[int, int, int, int], title: str, body: str, accent: str = CYAN, fill: str = PANEL) -> None:
    x, y, w, h = xywh
    d.rounded_rectangle((x, y, x + w, y + h), radius=18, fill=fill, outline="#2a3b56", width=3)
    d.rectangle((x, y, x + 8, y + h), fill=accent)
    d.text((x + 28, y + 23), title, fill=INK, font=F_LABEL)
    text_block(d, (x + 28, y + 64), body, width=max(24, int(w / 15)))


def pill(d: ImageDraw.ImageDraw, xy: tuple[int, int], text: str, color: str) -> None:
    x, y = xy
    tw = d.textlength(text, font=F_SMALL)
    d.rounded_rectangle((x, y, x + int(tw) + 54, y + 38), radius=19, fill="#0d1729", outline=color, width=2)
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


def draw_install_flow() -> None:
    img, d = canvas("Local model install flow", "From /install-local-gpu-server to a ready /local session")
    box(d, (70, 190, 270, 230), "User command", "/install-local-gpu-server\nor\nmilliwaysctl local install-gpu-server", GREEN)
    box(d, (430, 170, 300, 270), "Hardware probe", "NVIDIA: nvidia-smi\nAMD: DRM sysfs / rocm-smi\nmacOS: Apple Silicon unified memory", CYAN, PANEL_2)
    box(d, (820, 170, 300, 270), "Model pick", "Conservative VRAM budget:\nGGUF + KV cache + graph buffers + desktop headroom", PURPLE, PANEL_2)
    box(d, (1210, 190, 300, 230), "Service activation", "Download/cache GGUF\nwrite launcher\nrestart local server\nrestart daemon\nwait for socket", AMBER)
    arrow(d, (340, 305), (430, 305), GREEN)
    arrow(d, (730, 305), (820, 305), CYAN)
    arrow(d, (1120, 305), (1210, 305), PURPLE)
    pill(d, (430, 560), "CUDA when toolkit exists", GREEN)
    pill(d, (720, 560), "HIP when ROCm exists", PURPLE)
    pill(d, (985, 560), "Vulkan fallback", CYAN)
    pill(d, (1230, 560), "Metal on Apple GPU", AMBER)
    d.text((88, 740), "The important behavior: the installer does not print /local is ready until both the model endpoint and milliwaysd socket accept connections.", fill=INK, font=F_SUB)
    save(img, "local-model-install-flow.png")


def draw_hardware_matrix() -> None:
    img, d = canvas("GPU detection and acceleration", "The installer chooses an acceleration path that matches the machine")
    rows = [
        ("NVIDIA Linux", "nvidia-smi", "CUDA if nvcc/CUDA is present; otherwise Vulkan", GREEN),
        ("AMD Linux", "DRM sysfs or rocm-smi", "HIP if ROCm/HIP is present; otherwise Vulkan", PURPLE),
        ("Apple Silicon", "system_profiler + hw.memsize", "Metal, with unified-memory headroom", AMBER),
        ("Intel Mac dGPU", "system_profiler", "Metal for supported Apple graphics stack", BLUE),
        ("Unknown / CPU", "manual setup", "/setup-model or /install-local-server fallback", CYAN),
    ]
    y = 170
    for hw, probe, accel, color in rows:
        d.rounded_rectangle((80, y, 1520, y + 96), radius=18, fill=PANEL, outline="#2a3b56", width=2)
        d.text((116, y + 31), hw, fill=INK, font=F_LABEL)
        d.text((460, y + 33), probe, fill=MUTED, font=F_TEXT)
        pill(d, (870, y + 29), accel, color)
        y += 118
    save(img, "local-model-hardware-matrix.png")


def draw_model_selection() -> None:
    img, d = canvas("Curated local model selection", "Model choice is about fit, not only parameter count")
    rows = [
        ("Phi-3.5-mini", "2.2 GB", "smallest capable option; good on constrained laptops", GREEN),
        ("Mistral-7B-v0.3", "4.1 GB", "fast and light for quick edits", CYAN),
        ("Qwen2.5-Coder-7B", "4.7 GB", "strong coder; XML tool format translated by MilliWays", BLUE),
        ("Hermes-3 / Llama-3.1 8B", "4.9 GB", "native OpenAI tool calls; good agentic behavior", PURPLE),
        ("Qwen3-8B", "5.2 GB", "hybrid think/chat mode; selected on many 16 GB GPUs", AMBER),
        ("Qwen3-14B", "9.3 GB", "best all-round under 20B when memory allows", RED),
    ]
    y = 162
    for name, size, note, color in rows:
        d.rounded_rectangle((82, y, 1518, y + 88), radius=18, fill=PANEL, outline="#2a3b56", width=2)
        d.text((116, y + 27), name, fill=INK, font=F_LABEL)
        pill(d, (530, y + 25), size, color)
        d.text((760, y + 31), note, fill=MUTED, font=F_SMALL)
        y += 104
    d.text((98, 812), "Automatic GPU install uses about 45% of detected VRAM as a conservative budget. Larger models can still be installed manually.", fill=INK, font=F_TEXT)
    save(img, "local-model-selection.png")


def draw_tradeoffs() -> None:
    img, d = canvas("Local model tradeoffs", "The right default depends on privacy, latency, quality, memory, and operations")
    cols = [
        ((80, 180, 330, 500), "Small models", "Pros:\nfast startup\nlow memory\ncheap to keep hot\n\nCons:\nweaker reasoning\nmore brittle tool use\nless context headroom", GREEN),
        ((450, 180, 330, 500), "Mid-size coder", "Pros:\nbest laptop balance\ngood code edits\nusable tool loops\n\nCons:\nneeds GPU/RAM care\nslower first token\nquality varies by task", CYAN),
        ((820, 180, 330, 500), "Larger reasoning", "Pros:\nbetter planning\nstronger review\nhandles ambiguity\n\nCons:\nVRAM pressure\nlonger cold loads\nless portable", PURPLE),
        ((1190, 180, 330, 500), "Hosted fallback", "Pros:\nfrontier quality\nhuge context\nmanaged infra\n\nCons:\ncost\nnetwork dependency\nprivacy boundary", AMBER),
    ]
    for rect, title, body, color in cols:
        box(d, rect, title, body, color)
    d.text((90, 760), "MilliWays keeps local models first-class without pretending they replace hosted frontier models for every task.", fill=INK, font=F_SUB)
    save(img, "local-model-tradeoffs.png")


def main() -> None:
    draw_install_flow()
    draw_hardware_matrix()
    draw_model_selection()
    draw_tradeoffs()


if __name__ == "__main__":
    main()
