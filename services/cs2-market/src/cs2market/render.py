from __future__ import annotations

import json
import subprocess
from pathlib import Path


def render_short_videos(
    manifest_dir: Path,
    *,
    background: Path,
    out_dir: Path,
    ffmpeg: str = "ffmpeg",
    duration_seconds: float = 12.0,
) -> list[Path]:
    background = background.resolve()
    out_dir = out_dir.resolve()
    manifests = sorted(manifest_dir.glob("*.assets.json"))
    if not manifests:
        raise RuntimeError(f"no asset manifests found under {manifest_dir}")
    if not background.exists():
        raise RuntimeError(f"background image does not exist: {background}")
    out_dir.mkdir(parents=True, exist_ok=True)
    rendered: list[Path] = []
    for manifest in manifests:
        payload = json.loads(manifest.read_text(encoding="utf-8"))
        skin_image_path = str(payload["artifacts"].get("skin_image", "")).strip()
        if not skin_image_path:
            continue
        skin_image = Path(skin_image_path)
        if not skin_image.is_file():
            continue
        signal = payload["signal"]
        script = payload["short_script"]
        ass_path = manifest.with_suffix(".ass")
        ass_path.write_text(_ass_script(signal, script, duration_seconds), encoding="utf-8")
        output = out_dir / f"{manifest.stem.removesuffix('.assets')}.mp4"
        cmd = [
            ffmpeg,
            "-y",
            "-loop",
            "1",
            "-t",
            f"{duration_seconds:.3f}",
            "-i",
            str(background),
            "-loop",
            "1",
            "-t",
            f"{duration_seconds:.3f}",
            "-i",
            str(skin_image),
            "-f",
            "lavfi",
            "-t",
            f"{duration_seconds:.3f}",
            "-i",
            "anullsrc=channel_layout=stereo:sample_rate=48000",
            "-filter_complex",
            f"[0:v]scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920,setsar=1[bg];"
            f"[bg]drawbox=x=50:y=58:w=980:h=275:color=black@0.42:t=fill,"
            f"drawbox=x=50:y=58:w=980:h=4:color=0x37f7ff@0.85:t=fill,"
            f"drawbox=x=82:y=1394:w=916:h=272:color=black@0.56:t=fill,"
            f"drawbox=x=82:y=1394:w=916:h=3:color=0xffb547@0.85:t=fill,"
            f"drawbox=x=92:y=354:w=896:h=720:color=black@0.10:t=fill[bgbox];"
            f"[1:v]scale=880:-1,format=rgba,colorchannelmixer=aa=0.96[skin];"
            f"[bgbox][skin]overlay=(W-w)/2:540:format=auto,"
            f"subtitles={ass_path.name}[v]",
            "-map",
            "[v]",
            "-map",
            "2:a",
            "-r",
            "30",
            "-c:v",
            "libx264",
            "-pix_fmt",
            "yuv420p",
            "-preset",
            "veryfast",
            "-crf",
            "18",
            "-c:a",
            "aac",
            "-shortest",
            str(output),
        ]
        subprocess.run(cmd, cwd=manifest_dir, check=True, capture_output=True, text=True)
        rendered.append(output)
    return rendered


def _ass_script(signal: dict, script: dict, duration_seconds: float) -> str:
    end = _ass_time(duration_seconds)
    title = _ass_text(_wrap(_display_name(signal["market_hash_name"]), 28))
    action = _ass_text("BUY SIGNAL")
    price = _ass_text(f"{signal['price']:.2f} {signal['currency']}")
    score = _ass_text(f"SCORE {signal['score']:.1f}")
    confidence = _ass_text(f"CONF {signal['confidence']:.0%}")
    thesis_label = _ass_text("THESIS")
    thesis = _ass_text("High liquidity · 2-8 week swing")
    risk_label = _ass_text("RISK")
    risk = _ass_text("Fees + spread can invalidate")
    return f"""[Script Info]
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
WrapStyle: 0
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding
Style: Action,Arial,34,&H0008F7FF,&H000000FF,&HA0000000,&H90000000,1,0,0,0,100,100,1,0,1,2,1,8,80,80,92,1
Style: Title,Arial,54,&H00FFFFFF,&H000000FF,&HA0000000,&H90000000,1,0,0,0,100,100,0,0,1,3,1,8,80,80,138,1
Style: Metric,Arial,36,&H00E9FFE9,&H000000FF,&HA0000000,&H90000000,1,0,0,0,100,100,0,0,1,2,1,8,80,80,282,1
Style: Label,Arial,25,&H0008F7FF,&H000000FF,&HA0000000,&H90000000,1,0,0,0,100,100,1,0,1,1,0,7,122,122,1435,1
Style: Value,Arial,37,&H00FFFFFF,&H000000FF,&HA0000000,&H90000000,1,0,0,0,100,100,0,0,1,2,1,7,122,122,1472,1
Style: RiskLabel,Arial,25,&H0047B5FF,&H000000FF,&HA0000000,&H90000000,1,0,0,0,100,100,1,0,1,1,0,7,122,122,1548,1
Style: RiskValue,Arial,33,&H00DADADA,&H000000FF,&HA0000000,&H90000000,0,0,0,0,100,100,0,0,1,2,1,7,122,122,1585,1

[Events]
Format: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text
Dialogue: 0,0:00:00.00,{end},Action,,0,0,0,,{action}
Dialogue: 0,0:00:00.00,{end},Title,,0,0,0,,{title}
Dialogue: 0,0:00:00.00,{end},Metric,,0,0,0,,{price}   |   {score}   |   {confidence}
Dialogue: 0,0:00:00.00,{end},Label,,0,0,0,,{thesis_label}
Dialogue: 0,0:00:00.00,{end},Value,,0,0,0,,{thesis}
Dialogue: 0,0:00:00.00,{end},RiskLabel,,0,0,0,,{risk_label}
Dialogue: 0,0:00:00.00,{end},RiskValue,,0,0,0,,{risk}
"""


def _ass_text(value: str) -> str:
    return value.replace("\\", "\\\\").replace("{", r"\{").replace("}", r"\}").replace("\n", r"\N")


def _display_name(market_hash_name: str) -> str:
    return market_hash_name.replace(" (", "\n(")


def _wrap(value: str, width: int) -> str:
    lines: list[str] = []
    for raw_line in value.splitlines():
        words = raw_line.split()
        current = ""
        for word in words:
            candidate = f"{current} {word}".strip()
            if current and len(candidate) > width:
                lines.append(current)
                current = word
            else:
                current = candidate
        if current:
            lines.append(current)
    return "\n".join(lines)


def _ass_time(seconds: float) -> str:
    whole = int(seconds)
    centiseconds = int(round((seconds - whole) * 100))
    minutes, sec = divmod(whole, 60)
    hours, minutes = divmod(minutes, 60)
    return f"{hours}:{minutes:02d}:{sec:02d}.{centiseconds:02d}"
