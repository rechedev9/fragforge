import argparse
import bisect
import json
import math
import os
import subprocess
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


WIDTH = 1920
HEIGHT = 1080
FPS = 60


def load_font(size, bold=False):
    candidates = [
        r"C:\Windows\Fonts\arialbd.ttf" if bold else r"C:\Windows\Fonts\arial.ttf",
        r"C:\Windows\Fonts\segoeuib.ttf" if bold else r"C:\Windows\Fonts\segoeui.ttf",
    ]
    for path in candidates:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    return ImageFont.load_default()


FONT_XL = load_font(58, True)
FONT_L = load_font(38, True)
FONT_M = load_font(28, True)
FONT_S = load_font(22)
FONT_XS = load_font(18)


def team_color(team, target=False, alive=True):
    if target:
        base = (255, 211, 86)
    elif team == "CT":
        base = (88, 166, 255)
    else:
        base = (255, 92, 92)
    if alive:
        return base
    return tuple(max(35, int(c * 0.35)) for c in base)


def clean_name(name):
    if not name:
        return ""
    keep = []
    for ch in name:
        if ch.isascii() and (ch.isalnum() or ch in " ._-"):
            keep.append(ch)
    out = "".join(keep).strip()
    return out[:18] or "player"


def lerp(a, b, t):
    return a + (b - a) * t


def read_plan_segment(path, segment_id):
    with open(path, "r", encoding="utf-8") as f:
        plan = json.load(f)
    for segment in plan["segments"]:
        if segment["id"] == segment_id:
            return plan, segment
    raise SystemExit(f"segment not found: {segment_id}")


def player_map(frame):
    return {p["steamid64"]: p for p in frame["players"]}


def interpolate_player(prev, nxt, alpha):
    out = dict(prev)
    for key in ("x", "y", "z", "yaw"):
        out[key] = lerp(float(prev.get(key, 0)), float(nxt.get(key, 0)), alpha)
    out["alive"] = bool(nxt.get("alive", prev.get("alive", True)))
    out["health"] = int(nxt.get("health", prev.get("health", 0)))
    return out


def frame_at(frames, ticks, tick):
    idx = bisect.bisect_right(ticks, tick)
    if idx <= 0:
        return frames[0]["players"]
    if idx >= len(frames):
        return frames[-1]["players"]
    a = frames[idx - 1]
    b = frames[idx]
    span = max(1, b["tick"] - a["tick"])
    alpha = (tick - a["tick"]) / span
    amap = player_map(a)
    bmap = player_map(b)
    ids = sorted(set(amap) | set(bmap))
    players = []
    for sid in ids:
        if sid in amap and sid in bmap:
            players.append(interpolate_player(amap[sid], bmap[sid], alpha))
        elif sid in bmap:
            players.append(dict(bmap[sid]))
        else:
            players.append(dict(amap[sid]))
    return players


def build_transform(frames, segment):
    points = []
    kill_ids = set()
    for kill in segment["kills"]:
        kill_ids.add(kill["victim"]["steamid64"])
        points.append((kill["killer_pos"][0], kill["killer_pos"][1]))
        points.append((kill["victim_pos"][0], kill["victim_pos"][1]))

    for fr in frames:
        for p in fr["players"]:
            if p["steamid64"] in kill_ids or p.get("alive"):
                points.append((p["x"], p["y"]))

    min_x = min(x for x, _ in points) - 450
    max_x = max(x for x, _ in points) + 450
    min_y = min(y for _, y in points) - 450
    max_y = max(y for _, y in points) + 450
    world_w = max_x - min_x
    world_h = max_y - min_y
    target_ratio = WIDTH / HEIGHT
    if world_w / world_h > target_ratio:
        want_h = world_w / target_ratio
        pad = (want_h - world_h) / 2
        min_y -= pad
        max_y += pad
    else:
        want_w = world_h * target_ratio
        pad = (want_w - world_w) / 2
        min_x -= pad
        max_x += pad

    margin = 95
    scale = min((WIDTH - margin * 2) / (max_x - min_x), (HEIGHT - margin * 2) / (max_y - min_y))

    def to_screen(x, y):
        sx = margin + (x - min_x) * scale
        sy = HEIGHT - margin - (y - min_y) * scale
        return sx, sy

    return to_screen


def draw_background(draw):
    draw.rectangle((0, 0, WIDTH, HEIGHT), fill=(15, 18, 19))
    for i in range(0, WIDTH, 80):
        col = (28, 34, 35) if i % 240 else (38, 45, 46)
        draw.line((i, 0, i, HEIGHT), fill=col, width=1)
    for j in range(0, HEIGHT, 80):
        col = (28, 34, 35) if j % 240 else (38, 45, 46)
        draw.line((0, j, WIDTH, j), fill=col, width=1)
    draw.rectangle((54, 54, WIDTH - 54, HEIGHT - 54), outline=(75, 86, 84), width=2)


def draw_text(draw, xy, text, font, fill, anchor=None, stroke=0):
    draw.text(xy, text, font=font, fill=fill, anchor=anchor, stroke_width=stroke, stroke_fill=(0, 0, 0))


def draw_player(draw, p, to_screen, target_id):
    x, y = to_screen(p["x"], p["y"])
    target = p["steamid64"] == target_id
    alive = bool(p.get("alive", True))
    color = team_color(p.get("team"), target, alive)
    r = 17 if target else 13
    if not alive:
        r = 10
    draw.ellipse((x - r, y - r, x + r, y + r), fill=color, outline=(8, 10, 10), width=3)
    if target:
        draw.ellipse((x - 30, y - 30, x + 30, y + 30), outline=(255, 232, 128), width=3)
    if alive:
        yaw = math.radians(float(p.get("yaw", 0)) - 90)
        tip = (x + math.cos(yaw) * 31, y + math.sin(yaw) * 31)
        draw.line((x, y, tip[0], tip[1]), fill=color, width=4)
    label = "GranRa" if target else clean_name(p.get("name", ""))
    draw_text(draw, (x + 21, y - 10), label, FONT_XS, (224, 230, 226))


def render(args):
    with open(args.tracks, "r", encoding="utf-8") as f:
        tracks = json.load(f)
    plan, segment = read_plan_segment(args.plan, args.segment)
    target_id = plan["target"]["steamid64"]
    frames = tracks["frames"]
    ticks = [fr["tick"] for fr in frames]
    to_screen = build_transform(frames, segment)

    start_tick = segment["tick_start"]
    end_tick = segment["tick_end"]
    duration = args.duration
    frame_count = int(duration * FPS)
    kill_ticks = [k["tick"] for k in segment["kills"]]
    kill_by_tick = {k["tick"]: (i + 1, k) for i, k in enumerate(segment["kills"])}

    cmd = [
        "ffmpeg", "-y",
        "-f", "rawvideo", "-pix_fmt", "rgb24", "-s", f"{WIDTH}x{HEIGHT}", "-r", str(FPS), "-i", "-",
        "-f", "lavfi", "-i", f"anullsrc=channel_layout=stereo:sample_rate=44100",
        "-shortest", "-c:v", "libx264", "-preset", "fast", "-crf", "18",
        "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart",
        args.out,
    ]
    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE)
    assert proc.stdin is not None

    trails = {target_id: []}
    for n in range(frame_count):
        pct = n / max(1, frame_count - 1)
        tick = start_tick + (end_tick - start_tick) * pct
        players = frame_at(frames, ticks, tick)
        img = Image.new("RGB", (WIDTH, HEIGHT), (15, 18, 19))
        draw = ImageDraw.Draw(img)
        draw_background(draw)

        target_player = next((p for p in players if p["steamid64"] == target_id), None)
        if target_player:
            trails[target_id].append(to_screen(target_player["x"], target_player["y"]))
            trails[target_id] = trails[target_id][-90:]
        if len(trails[target_id]) > 1:
            for i in range(1, len(trails[target_id])):
                alpha = int(40 + 150 * i / len(trails[target_id]))
                draw.line((*trails[target_id][i - 1], *trails[target_id][i]), fill=(alpha, 150, 88), width=5)

        for p in sorted(players, key=lambda p: p["steamid64"] == target_id):
            draw_player(draw, p, to_screen, target_id)

        completed = sum(1 for kt in kill_ticks if kt <= tick)
        next_kill = next((kt for kt in kill_ticks if kt >= tick), kill_ticks[-1])
        pulse = 0
        active_kill = None
        for kt in kill_ticks:
            dist = abs(tick - kt)
            if dist <= 34:
                pulse = max(pulse, 1 - dist / 34)
                active_kill = kt
        if active_kill is not None:
            _, kill = kill_by_tick[active_kill]
            kx, ky = to_screen(kill["killer_pos"][0], kill["killer_pos"][1])
            vx, vy = to_screen(kill["victim_pos"][0], kill["victim_pos"][1])
            width = max(4, int(14 * pulse))
            draw.line((kx, ky, vx, vy), fill=(255, 226, 95), width=width)
            rr = 24 + int(52 * pulse)
            draw.ellipse((vx - rr, vy - rr, vx + rr, vy + rr), outline=(255, 95, 95), width=5)
            idx, _ = kill_by_tick[active_kill]
            draw_text(draw, (vx, vy - rr - 32), f"KILL {idx}/5", FONT_M, (255, 238, 160), anchor="mm", stroke=2)

        draw_text(draw, (70, 70), "GRANRA ACE REPLAY", FONT_XL, (245, 246, 240))
        draw_text(draw, (72, 140), "ROUND 16 - OVERHEAD TACTICAL VIEW", FONT_M, (172, 188, 186))
        draw_text(draw, (WIDTH - 70, 74), f"{completed}/5", FONT_XL, (255, 211, 86), anchor="ra")
        draw_text(draw, (WIDTH - 70, 138), "KILLS", FONT_S, (178, 184, 180), anchor="ra")

        bar_x0, bar_y0, bar_x1, bar_y1 = 70, HEIGHT - 85, WIDTH - 70, HEIGHT - 58
        draw.rounded_rectangle((bar_x0, bar_y0, bar_x1, bar_y1), radius=10, fill=(35, 41, 42), outline=(82, 91, 89), width=1)
        draw.rounded_rectangle((bar_x0, bar_y0, bar_x0 + (bar_x1 - bar_x0) * pct, bar_y1), radius=10, fill=(255, 211, 86))
        for kt in kill_ticks:
            kp = (kt - start_tick) / (end_tick - start_tick)
            x = bar_x0 + (bar_x1 - bar_x0) * kp
            draw.line((x, bar_y0 - 10, x, bar_y1 + 10), fill=(255, 92, 92), width=4)
        if next_kill:
            seconds = max(0, (next_kill - tick) / tracks["tickrate"])
            draw_text(draw, (WIDTH - 70, HEIGHT - 123), f"NEXT IMPACT {seconds:0.1f}s", FONT_S, (210, 218, 214), anchor="ra")

        proc.stdin.write(img.tobytes())

    proc.stdin.close()
    if proc.wait() != 0:
        raise SystemExit("ffmpeg failed while rendering tactical replay")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tracks", required=True)
    ap.add_argument("--plan", required=True)
    ap.add_argument("--segment", default="seg-008")
    ap.add_argument("--out", required=True)
    ap.add_argument("--duration", type=float, default=7.0)
    args = ap.parse_args()
    Path(args.out).parent.mkdir(parents=True, exist_ok=True)
    render(args)


if __name__ == "__main__":
    main()
