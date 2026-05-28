from __future__ import annotations

import json
from pathlib import Path

from .models import ShortScript, Signal, isoformat


def build_short_script(
    signal: Signal,
    *,
    skin_image_path: Path | None = None,
    asset_manifest_path: Path | None = None,
) -> ShortScript:
    action_label = signal.action.upper()
    price = f"{signal.price:.2f} {signal.currency}" if signal.price is not None else "precio no disponible"
    confidence = f"{signal.confidence:.0%}"
    title = f"{action_label} CS2: {signal.market_hash_name}"
    hook = f"Senal {action_label} para {signal.market_hash_name}: precio publico {price}, confianza {confidence}."
    reasons = " ".join(signal.reasons[:3])
    risks = " ".join(signal.risks[:2])
    narration = (
        f"Hoy el modelo marca {action_label} en {signal.market_hash_name}. "
        f"La tesis es para un swing de {signal.horizon}, no para holdear a ciegas. "
        f"{reasons} "
        f"El riesgo principal: {risks} "
        "Si cambia la liquidez o el spread, la senal queda invalidada."
    )
    disclaimer = "Investigacion de mercado CS2; no es garantia de beneficio ni asesoramiento financiero."
    gpt_image_prompt = _gpt_image_prompt(signal, price, confidence)
    caption = (
        f"{title}\n\n"
        f"Precio de referencia: {price}\n"
        f"Score: {signal.score:.1f}/100 | Confianza: {confidence}\n"
        f"Horizonte: {signal.horizon}\n"
        f"{disclaimer}\n\n"
        "#CS2 #CS2Skins #CounterStrike2 #CS2Market #Gaming"
    )
    return ShortScript(
        signal_id=signal.id,
        market_hash_name=signal.market_hash_name,
        action=signal.action,
        title=title,
        hook=hook,
        narration=narration,
        caption=caption,
        disclaimer=disclaimer,
        gpt_image_prompt=gpt_image_prompt,
        skin_image_path=str(skin_image_path or ""),
        asset_manifest_path=str(asset_manifest_path or ""),
    )


def export_short_scripts(signals: list[Signal], out_dir: Path, image_paths: dict[str, Path] | None = None) -> list[Path]:
    stamp = isoformat().replace(":", "").replace("-", "").replace("Z", "")
    target_dir = out_dir / stamp[:8]
    target_dir.mkdir(parents=True, exist_ok=True)
    written: list[Path] = []
    image_paths = image_paths or {}
    for index, signal in enumerate(signals, start=1):
        slug = _slug(signal.market_hash_name)
        base = target_dir / f"{index:02d}_{signal.action}_{slug}"
        manifest_path = base.with_suffix(".assets.json")
        script = build_short_script(
            signal,
            skin_image_path=image_paths.get(signal.market_hash_name),
            asset_manifest_path=manifest_path,
        )
        md_path = base.with_suffix(".md")
        caption_path = base.with_suffix(".caption.txt")
        json_path = base.with_suffix(".json")
        prompt_path = base.with_suffix(".gpt-image-prompt.txt")
        md_path.write_text(_markdown(script, signal), encoding="utf-8")
        caption_path.write_text(script.caption + "\n", encoding="utf-8")
        json_path.write_text(json.dumps(script.to_dict(), indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        prompt_path.write_text(script.gpt_image_prompt + "\n", encoding="utf-8")
        manifest_path.write_text(json.dumps(_asset_manifest(script, signal), indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        written.extend([md_path, caption_path, json_path, prompt_path, manifest_path])
    return written


def _markdown(script: ShortScript, signal: Signal) -> str:
    reasons = "\n".join(f"- {reason}" for reason in signal.reasons)
    risks = "\n".join(f"- {risk}" for risk in signal.risks)
    return f"""# {script.title}

Hook:
{script.hook}

Narration:
{script.narration}

Reasons:
{reasons}

Risks:
{risks}

Caption:
{script.caption}

GPT Image prompt:
{script.gpt_image_prompt}
"""


def _gpt_image_prompt(signal: Signal, price: str, confidence: str) -> str:
    action = signal.action.upper()
    return (
        "Use case: ads-marketing\n"
        "Asset type: 9:16 YouTube Shorts background/thumbnail for CS2 market analysis\n"
        f"Primary request: premium esports trading card style visual for {signal.market_hash_name}\n"
        "Style/medium: polished 3D product render, dark competitive gaming studio, finance dashboard accents\n"
        "Composition/framing: vertical 9:16, centered item pedestal, large clean space at top for title overlay\n"
        f"Text (verbatim): \"{action} | {price} | {confidence}\"\n"
        "Constraints: no Valve logo, no marketplace logos, no guarantee language, no fake chart numbers, no watermark\n"
        "Avoid: cluttered UI, unreadable text, real currency cash piles, gambling imagery\n"
    )


def _asset_manifest(script: ShortScript, signal: Signal) -> dict:
    return {
        "signal": signal.to_dict(),
        "short_script": script.to_dict(),
        "artifacts": {
            "skin_image": script.skin_image_path,
            "gpt_image_prompt": script.gpt_image_prompt,
        },
        "production_notes": [
            "Use skin_image as reference/foreground when creating the final Short visual.",
            "Keep score, confidence, timestamp, and risks visible in captions or narration.",
            "Do not present the signal as guaranteed profit.",
        ],
    }


def _slug(value: str) -> str:
    out = []
    last_underscore = False
    for char in value.lower():
        if char.isascii() and char.isalnum():
            out.append(char)
            last_underscore = False
        elif not last_underscore:
            out.append("_")
            last_underscore = True
    return "".join(out).strip("_")[:80] or "signal"
