#!/usr/bin/env python3
"""Explicit native CPU-runtime preparation; never launches the app or model."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
WORKER = ROOT / "workers" / "background"
MANIFEST = WORKER / "runtime-manifest.json"
LOCK = WORKER / "requirements.lock"


def write_json(path, value):
    handle, temporary = tempfile.mkstemp(prefix=".setup-", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(handle, "w", encoding="utf-8") as target:
            json.dump(value, target, ensure_ascii=False, indent=2)
            target.write("\n")
            target.flush()
            os.fsync(target.fileno())
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def prepare_model(destination, model):
    if destination.is_symlink():
        raise RuntimeError("Model path must not be a symbolic link")
    if destination.exists():
        with destination.open("rb") as source:
            digest = hashlib.file_digest(source, "sha256").hexdigest()
        if destination.stat().st_size != model["bytes"] or digest != model["sha256"]:
            raise RuntimeError("Existing model does not match the manifest; remove only that model file and retry")
        return
    handle, temporary = tempfile.mkstemp(prefix=".u2netp-", suffix=".part", dir=destination.parent)
    try:
        request = urllib.request.Request(model["url"], headers={"User-Agent": "ScreenShortsPro-runtime-setup"})
        digest = hashlib.sha256()
        size = 0
        with os.fdopen(handle, "wb") as target, urllib.request.urlopen(request, timeout=60) as source:
            while chunk := source.read(1024 * 1024):
                size += len(chunk)
                if size > model["bytes"]:
                    raise RuntimeError("Downloaded model exceeds the manifest size")
                digest.update(chunk)
                target.write(chunk)
            target.flush()
            os.fsync(target.fileno())
        if size != model["bytes"] or digest.hexdigest() != model["sha256"]:
            raise RuntimeError("Model SHA-256/size mismatch; nothing installed")
        os.replace(temporary, destination)
    finally:
        Path(temporary).unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description="Prepare local CPU background removal (downloads required)")
    parser.add_argument("--base-config", type=Path, help="Existing OCR/runtime profile to preserve")
    args = parser.parse_args()
    target_os = {"linux": "linux", "win32": "windows"}.get(sys.platform)
    if target_os is None or platform.machine().lower() not in ("x86_64", "amd64"):
        raise RuntimeError("Prepare natively on Linux x86_64 or Windows amd64")
    if sys.version_info[:2] not in ((3, 12), (3, 13)):
        raise RuntimeError("Use CPython 3.12 or 3.13; other versions have not been selected for this runtime")
    if target_os == "linux" and not Path(sys.base_prefix).resolve().is_relative_to(Path("/usr")):
        raise RuntimeError("Use a system CPython under /usr (including /usr/local); custom prefixes need a separate sandbox adapter")
    base = (args.base_config or ROOT / "config" / f"runtime-{target_os}.json").resolve()
    if base.stat().st_size > 65536:
        raise RuntimeError("Base profile exceeds 64 KiB")
    profile = json.loads(base.read_text(encoding="utf-8"))
    if profile.get("schema") != "skrinpolka-runtime-v1":
        raise RuntimeError("Unsupported base profile")
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    directory = ROOT / ".local-data" / f"background-{target_os}"
    marker = directory / "runtime-owner.json"
    directory.mkdir(parents=True, exist_ok=True)
    if marker.exists():
        owner = json.loads(marker.read_text(encoding="utf-8"))
        if owner.get("schema") != "screenshorts-background-setup-v1" or owner.get("python") != list(sys.version_info[:2]):
            raise RuntimeError("Existing runtime belongs to a different setup/Python version; use its original interpreter")
    elif any(directory.iterdir()):
        raise RuntimeError("Runtime directory is not empty and has no setup marker; it will not be modified")
    else:
        write_json(marker, {"schema": "screenshorts-background-setup-v1", "python": list(sys.version_info[:2])})

    environment = directory / ".venv"
    executable = environment / ("Scripts/python.exe" if target_os == "windows" else "bin/python")
    if not executable.exists():
        subprocess.run([sys.executable, "-m", "venv", str(environment)], check=True)
    subprocess.run([
        str(executable), "-m", "pip", "install", "--disable-pip-version-check",
        "--only-binary=:all:", "--require-hashes", "--no-input", "-r", str(LOCK),
    ], check=True)
    models = directory / "models"
    models.mkdir(exist_ok=True)
    model_path = models / "u2netp.onnx"
    prepare_model(model_path, manifest["model"])

    # Keep an existing OCR profile usable after moving the generated JSON.
    ocr = profile.get("ocr", {})
    def absolute(value):
        path = Path(value)
        return str(path if path.is_absolute() else (base.parent / path).resolve())
    for key in ("executable", "tessdata_directory"):
        if ocr.get(key):
            ocr[key] = absolute(ocr[key])
    ocr["read_roots"] = [absolute(value) for value in ocr.get("read_roots", [])]
    profile["ocr"] = ocr
    profile["background_removal"] = {
        "enabled": True, "executable": str(executable),
        "script": str(WORKER / "remove_background.py"), "model_path": str(model_path),
        "model_sha256": manifest["model"]["sha256"], "model_id": "u2netp",
        "read_roots": list(dict.fromkeys([str(directory), str(WORKER), sys.base_prefix])),
        "memory_bytes": 1073741824, "threads": 2, "timeout_seconds": 120,
    }
    generated = directory / f"runtime-{target_os}.json"
    write_json(generated, profile)
    write_json(directory / "runtime-receipt.json", {
        "schema": "screenshorts-background-receipt-v1", "python": sys.version,
        "model": manifest["model"], "packages": manifest["packages"],
        "requirements_sha256": hashlib.sha256(LOCK.read_bytes()).hexdigest(),
        "worker_sha256": hashlib.sha256((WORKER / "remove_background.py").read_bytes()).hexdigest(),
        "base_config": str(base), "runtime_config": str(generated),
    })
    print(f"Components prepared. Runtime profile: {generated}")
    print("The application, inference, OCR and tests were NOT started.")
    print("Restart with --runtime-config pointing to this generated profile.")


if __name__ == "__main__":
    main()
