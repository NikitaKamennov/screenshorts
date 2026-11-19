#!/usr/bin/env python3
"""ScreenShorts Pro local CPU worker. No downloads or network API.

U2NETP input normalization follows the published U-2-Net/rembg model contract.
Only the flattened PNG supplied by the host is read, never a capture history.
"""

import argparse
import hashlib
import os
from pathlib import Path
import warnings


MAX_BYTES = 20 * 1024 * 1024
MAX_PIXELS = 16_000_000


def main():
    parser = argparse.ArgumentParser(description="Local U2NETP background removal")
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--threads", type=int, required=True)
    args = parser.parse_args()
    if not 1 <= args.threads <= 4:
        raise ValueError("Invalid thread budget")
    for key in ("OMP_NUM_THREADS", "OMP_THREAD_LIMIT", "OPENBLAS_NUM_THREADS"):
        os.environ[key] = str(args.threads)

    # Import native libraries only inside the isolated worker, with thread
    # budgets already set. The host's readiness call does not import/run them.
    import numpy as np
    import onnxruntime as ort
    from PIL import Image

    Image.MAX_IMAGE_PIXELS = MAX_PIXELS
    warnings.simplefilter("error", Image.DecompressionBombWarning)
    if args.input.stat().st_size > MAX_BYTES:
        raise ValueError("Input PNG exceeds 20 MiB")
    if args.model.stat().st_size > 8 * 1024 * 1024:
        raise ValueError("Unexpected model size")
    with args.model.open("rb") as source:
        digest = hashlib.file_digest(source, "sha256").hexdigest()
    if digest.lower() != args.sha256.lower():
        raise ValueError("Model SHA-256 mismatch")

    with Image.open(args.input) as source:
        width, height = source.size
        if source.format != "PNG" or width < 1 or height < 1 or width * height > MAX_PIXELS:
            raise ValueError("Expected a bounded PNG")
        original = source.convert("RGBA")

    # Transparent RGB is never used as image content. Composite on white for
    # inference; the original alpha will still constrain the final mask.
    visible = Image.new("RGBA", original.size, (255, 255, 255, 255))
    visible.alpha_composite(original)
    resized = visible.convert("RGB").resize((320, 320), Image.Resampling.LANCZOS)
    pixels = np.asarray(resized, dtype=np.float32)
    pixels /= max(float(pixels.max()), 1e-6)
    pixels -= np.array([0.485, 0.456, 0.406], dtype=np.float32)
    pixels /= np.array([0.229, 0.224, 0.225], dtype=np.float32)
    tensor = np.ascontiguousarray(pixels.transpose(2, 0, 1)[None])

    options = ort.SessionOptions()
    options.intra_op_num_threads = args.threads
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    options.enable_mem_pattern = False
    options.log_severity_level = 3
    options.add_session_config_entry("session.intra_op.allow_spinning", "0")
    options.add_session_config_entry("session.inter_op.allow_spinning", "0")
    session = ort.InferenceSession(str(args.model), sess_options=options, providers=["CPUExecutionProvider"])
    session.disable_fallback()
    if session.get_providers() != ["CPUExecutionProvider"]:
        raise ValueError("Expected CPU provider only")
    inputs = session.get_inputs()
    if len(inputs) != 1:
        raise ValueError("Unexpected U2NETP input contract")
    prediction = session.run([session.get_outputs()[0].name], {inputs[0].name: tensor})[0]
    mask = np.asarray(prediction, dtype=np.float32).squeeze()
    if mask.shape != (320, 320) or not np.isfinite(mask).all():
        raise ValueError("Invalid foreground mask")
    low, high = float(mask.min()), float(mask.max())
    if high - low < 1e-6:
        raise ValueError("The model could not distinguish foreground from background")
    mask = np.clip((mask - low) / (high - low), 0, 1)
    alpha = Image.fromarray(np.rint(mask * 255).astype(np.uint8)).resize(original.size, Image.Resampling.LANCZOS)
    rgba = np.array(original, dtype=np.uint8)
    combined = (rgba[:, :, 3].astype(np.uint16) * np.asarray(alpha, dtype=np.uint16) + 127) // 255
    rgba[:, :, 3] = combined.astype(np.uint8)
    rgba[rgba[:, :, 3] == 0, :3] = 0
    result = Image.fromarray(rgba)
    # Metadata and source chunks are not copied. The host validates dimensions,
    # byte limit and flattened pixels again before accepting this output.
    result.save(args.output, format="PNG")
    if args.output.stat().st_size > MAX_BYTES:
        args.output.unlink()
        raise ValueError("Result PNG exceeds 20 MiB")


if __name__ == "__main__":
    main()
