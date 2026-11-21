#!/usr/bin/env python3
"""Build a relocatable CPU/OCR/GTK folder and a FUSE-free .run; no app or tests."""
import argparse
from datetime import datetime, timezone
import fcntl
import functools
import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
BASE = ROOT / ".local-data/linux-x64"
CACHE = BASE / "cache"
MULTI = "x86_64-linux-gnu"
BASE_GLIBC = re.compile(r"^(ld-linux|lib(c|m|mvec|pthread|thread_db|memusage|pcprofile|dl|rt|resolv|util|anl|nss_[^.]+|BrokenLocale))[-.]")
PACKAGE_VERSIONS = {}
COPYRIGHTS = set()


def run(args, **kwargs):
    return subprocess.run([str(value) for value in args], check=True, **kwargs)


def output(args, **kwargs):
    return run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, **kwargs).stdout


def sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".part")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    os.replace(temporary, path)


def owned_directory(path):
    path = Path(path)
    current = path
    while current != ROOT:
        if current.is_symlink():
            raise RuntimeError(f"Refusing a symlink in build path: {current}")
        if current == current.parent:
            raise RuntimeError("Build output is outside the project")
        current = current.parent
    path.mkdir(parents=True, exist_ok=True)
    return path


def version(package):
    if package not in PACKAGE_VERSIONS:
        PACKAGE_VERSIONS[package] = output(["dpkg-query", "-W", "-f=${Version}", package]).strip()
    return PACKAGE_VERSIONS[package]


def copyright(package, target):
    if package in COPYRIGHTS:
        return
    source = Path("/usr/share/doc") / package.split(":")[0] / "copyright"
    if not source.is_file():
        raise RuntimeError(f"Missing distribution copyright: {package}")
    destination = target / "licenses/system" / package.replace(":", "_") / "copyright"
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, destination)
    version(package)
    COPYRIGHTS.add(package)


@functools.lru_cache(maxsize=None)
def owner(path):
    candidates = [str(path), str(Path(path).resolve())]
    for value in list(candidates):
        if value.startswith("/usr/lib/"):
            candidates.append(value[4:])
    for value in candidates:
        result = subprocess.run(["dpkg-query", "-S", value], capture_output=True, text=True)
        if result.returncode == 0:
            for line in result.stdout.splitlines():
                if ": " in line and not line.startswith("diversion"):
                    return line.rsplit(": ", 1)[0].split(", ")[0]
    raise RuntimeError(f"Cannot attribute native runtime file: {path}")


def copy_file(source, destination):
    source, destination = Path(source), Path(destination)
    if not source.is_file():
        return
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists():
        if sha(source) != sha(destination):
            raise RuntimeError(f"Conflicting runtime files: {destination}")
        return
    shutil.copyfile(source, destination)
    destination.chmod(0o755 if os.access(source, os.X_OK) else 0o644)


def copy_package(package, target):
    copyright(package, target)
    for value in output(["dpkg-query", "-L", package]).splitlines():
        path = Path(value)
        if not value.startswith("/usr/") or not path.is_file():
            continue
        if value.startswith(("/usr/share/doc/", "/usr/share/man/", "/usr/share/lintian/", "/usr/share/bug/")):
            continue
        copy_file(path, target / "runtime" / value.lstrip("/"))


def elf(path):
    if not path.is_file():
        return False
    with path.open("rb") as stream:
        return stream.read(4) == b"\x7fELF"


def native_dependencies(target):
    libraries = {}
    for line in output(["/sbin/ldconfig", "-p"]).splitlines():
        match = re.match(r"\s*(\S+)\s+\(([^)]+)\)\s+=>\s+(\S+)", line)
        if match and "x86-64" in match[2]:
            libraries.setdefault(match[1], Path(match[3]))
    pending = [path for path in (target / "runtime/usr").rglob("*") if elf(path)]
    visited = set()
    while pending:
        path = pending.pop()
        if path in visited:
            continue
        visited.add(path)
        dynamic = output(["readelf", "-d", path])
        if len(visited) % 64 == 0:
            print(f"Resolving native libraries: {len(visited)} ELF files", flush=True)
        origin = "/" + str(path.parent.relative_to(target / "runtime"))
        search = []
        for entry in re.findall(r"\((?:RUNPATH|RPATH)\).*?\[(.*?)\]", dynamic):
            for value in entry.split(":"):
                search.append(Path(value.replace("$" + "{ORIGIN}", origin).replace("$ORIGIN", origin)))
        for name in re.findall(r"\(NEEDED\).*?\[(.*?)\]", dynamic):
            source = libraries.get(name)
            if source is None:
                source = next((directory / name for directory in search
                               if (directory / name).is_file() and str(directory).startswith(("/usr/lib/", "/lib/"))), None)
            if source is None:
                matches = list((target / "runtime/usr").rglob(name))
                if matches:
                    pending.extend(matches)
                    continue
                raise RuntimeError(f"Unresolved ELF dependency {name} of {path}")
            copyright(owner(source), target)
            destination = target / "runtime/usr/lib" / MULTI / name
            fresh = not destination.exists()
            copy_file(source, destination)
            if fresh:
                pending.append(destination)
    lib = target / "runtime/lib"
    lib.mkdir(parents=True, exist_ok=True)
    for path in (target / "runtime/usr/lib" / MULTI).iterdir():
        if path.is_file() and ".so" in path.name and not BASE_GLIBC.match(path.name):
            os.link(path, lib / path.name)
    copy_file("/lib64/ld-linux-x86-64.so.2", target / "runtime/usr/lib64/ld-linux-x86-64.so.2")
    copyright(owner("/lib64/ld-linux-x86-64.so.2"), target)


def prepare_wayland_clipboard(target, offline):
    directory = owned_directory(CACHE / "downloads")
    receipt = directory / "wl-clipboard.json"
    if receipt.is_file():
        record = json.loads(receipt.read_text())
        archive = directory / record["filename"]
        if not archive.is_file() or sha(archive) != record["sha256"]:
            raise RuntimeError("Cached wl-clipboard SHA mismatch")
    else:
        if offline:
            raise RuntimeError("wl-clipboard cache missing; first build needs network")
        metadata = output(["apt-cache", "show", "wl-clipboard"])
        record = {}
        for line in metadata.split("\n\n")[0].splitlines():
            key, separator, value = line.partition(": ")
            if separator:
                record[key] = value
        selected = record["Version"]
        run(["apt-get", "download", "wl-clipboard=" + selected], cwd=directory)
        archive = next((item for item in directory.glob("wl-clipboard_*.deb") if sha(item) == record["SHA256"]), None)
        if archive is None:
            raise RuntimeError("wl-clipboard SHA mismatch against APT metadata")
        record = {"version": selected, "filename": archive.name, "sha256": sha(archive),
                  "source": "APT wl-clipboard binary package; SHA256 from configured signed repository metadata"}
        write_json(receipt, record)
    unpacked = directory / ("wl-clipboard-" + record["sha256"][:16])
    if not unpacked.exists():
        run(["dpkg-deb", "-x", archive, unpacked])
    for path in (unpacked / "usr").rglob("*"):
        if path.is_file():
            if "/share/doc/" in str(path):
                if path.name == "copyright":
                    copy_file(path, target / "licenses/system/wl-clipboard/copyright")
            elif "/share/man/" not in str(path):
                copy_file(path, target / "runtime/usr" / path.relative_to(unpacked / "usr"))
    return record


def model_file(destination, metadata, offline):
    cache = owned_directory(CACHE / "models") / "u2netp.onnx"
    if not cache.exists():
        previous = ROOT / ".local-data/background-linux/models/u2netp.onnx"
        if previous.is_file() and sha(previous) == metadata["sha256"]:
            copy_file(previous, cache)
        elif offline:
            raise RuntimeError("Model cache missing; first build needs network")
        else:
            temporary = cache.with_suffix(".part")
            total = 0
            try:
                with urllib.request.urlopen(metadata["url"], timeout=60) as source, temporary.open("wb") as result:
                    while block := source.read(1024 * 1024):
                        total += len(block)
                        if total > metadata["bytes"]:
                            raise RuntimeError("Model exceeds manifest size")
                        result.write(block)
                if total != metadata["bytes"] or sha(temporary) != metadata["sha256"]:
                    raise RuntimeError("Model SHA-256/size mismatch")
                os.replace(temporary, cache)
            finally:
                temporary.unlink(missing_ok=True)
    if cache.stat().st_size != metadata["bytes"] or sha(cache) != metadata["sha256"]:
        raise RuntimeError("Cached model SHA-256/size mismatch")
    copy_file(cache, destination)


def python_packages(target, offline):
    builder = CACHE / "build-python"
    if not builder.exists():
        run([sys.executable, "-m", "venv", builder])
        write_json(builder / "screenshorts-build-owner.json", {"schema": "screenshorts-linux-builder-v1"})
    if not (builder / "screenshorts-build-owner.json").is_file():
        raise RuntimeError("Refusing an unowned Python build environment")
    wheels = owned_directory(CACHE / "wheels")
    python = builder / "bin/python"
    lock = ROOT / "workers/background/requirements.lock"
    if not offline:
        run([python, "-m", "pip", "download", "--disable-pip-version-check", "--only-binary=:all:",
             "--require-hashes", "-r", lock, "--dest", wheels])
    run([python, "-m", "pip", "install", "--disable-pip-version-check", "--no-index", "--find-links", wheels,
         "--only-binary=:all:", "--require-hashes", "--no-compile", "--no-deps",
         "--target", target / "runtime/usr/lib/python3/dist-packages", "-r", lock])


def patch_webkit(target):
    patches = []
    for library in (target / "runtime/usr/lib" / MULTI).glob("libwebkit2gtk-4.1.so*"):
        data = library.read_bytes()
        original = hashlib.sha256(data).hexdigest()
        replacements = []
        for old, new in [(b"/usr/bin/bwrap\x00", b"bwrap\x00"), (b"/usr/bin/xdg-dbus-proxy\x00", b"xdg-dbus-proxy\x00")]:
            count = data.count(old)
            if count:
                data = data.replace(old, new.ljust(len(old), b"\x00"))
                replacements.append({"old": old[:-1].decode(), "new": new[:-1].decode(), "count": count})
        if replacements:
            library.write_bytes(data)
            patches.append({"path": str(library.relative_to(target)), "original_sha256": original,
                            "sha256": sha(library), "replacements": replacements})
    if not patches:
        raise RuntimeError("WebKit helper paths not found; review upstream before packaging")
    return patches


def deduplicate(target):
    seen = {}
    for path in sorted(target.rglob("*")):
        if path.is_file():
            key = (path.stat().st_size, path.stat().st_mode & 0o777, sha(path))
            if key in seen:
                if path.stat().st_ino != seen[key].stat().st_ino:
                    path.unlink()
                    os.link(seen[key], path)
            else:
                seen[key] = path


def runtime_fingerprint(config):
    digest = hashlib.sha256()
    digest.update(b"screenshorts-linux-runtime-layout-v1")
    for path in [ROOT / "config/linux-release.json", ROOT / "workers/background/requirements.lock",
                 ROOT / "workers/background/runtime-manifest.json"]:
        digest.update(path.read_bytes())
    for package in config["packages"] + ["libc6"]:
        digest.update((package + "=" + version(package)).encode())
    return digest.hexdigest()


def prepare_runtime(config, offline, refresh):
    destination = CACHE / "runtime"
    fingerprint = runtime_fingerprint(config)
    if destination.is_dir() and not refresh:
        receipt = json.loads((destination / "runtime-manifest.json").read_text())
        if receipt["fingerprint"] != fingerprint:
            raise RuntimeError("Runtime inputs changed. Rebuild with --refresh-runtime.")
        for package, pinned in receipt["system_packages"].items():
            if version(package) != pinned:
                raise RuntimeError(f"{package} changed; use --refresh-runtime")
        for record in receipt["files"]:
            path = destination / record["path"]
            if not path.is_file() or sha(path) != record["sha256"]:
                raise RuntimeError(f"Cached runtime changed: {record['path']}; refresh explicitly")
        print("Reusing prepared native/Python/model runtime", flush=True)
        return destination
    temporary = Path(tempfile.mkdtemp(prefix="runtime-stage-", dir=CACHE))
    try:
        for package in config["packages"] + ["libc6"]:
            print("Collecting", package, flush=True)
            copy_package(package, temporary)
        clipboard = prepare_wayland_clipboard(temporary, offline)
        python_packages(temporary, offline)
        metadata = json.loads((ROOT / "workers/background/runtime-manifest.json").read_text())
        model_file(temporary / "models/background/u2netp.onnx", metadata["model"], offline)
        native_dependencies(temporary)
        patches = patch_webkit(temporary)
        shutil.copytree("/usr/share/common-licenses", temporary / "licenses/common", dirs_exist_ok=True)
        shutil.copytree("/usr/share/common-licenses", temporary / "runtime/usr/share/common-licenses", dirs_exist_ok=True)
        run(["glib-compile-schemas", temporary / "runtime/usr/share/glib-2.0/schemas"])
        run(["gio-querymodules", temporary / "runtime/usr/lib" / MULTI / "gio/modules"])
        fonts = temporary / "runtime/etc/fonts/fonts.conf"
        fonts.parent.mkdir(parents=True, exist_ok=True)
        fonts.write_text('<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">'
                         '<fontconfig><dir prefix="relative">../../usr/share/fonts</dir>'
                         '<cachedir prefix="xdg">skrinpolka/fontconfig</cachedir></fontconfig>\n')
        deduplicate(temporary)
        receipt = {"schema": "screenshorts-linux-runtime-v1", "fingerprint": fingerprint,
                   "system_packages": dict(sorted(PACKAGE_VERSIONS.items())), "wl_clipboard": clipboard,
                   "model": metadata["model"], "python_lock_sha256": sha(ROOT / "workers/background/requirements.lock"),
                   "python_packages": metadata["packages"],
                   "webkit_relocation_patches": patches,
                   "source_packages": "Ubuntu/Mint installed binary packages; original copyright notices included",
                   "base_glibc": "GUI uses host glibc >= 2.39; workers use bundled glibc in private /usr",
                   "files": [{"path": str(path.relative_to(temporary)), "sha256": sha(path)}
                             for path in sorted(temporary.rglob("*")) if path.is_file()]}
        write_json(temporary / "runtime-manifest.json", receipt)
        if destination.exists():
            destination.rename(CACHE / ("runtime-previous-" + uuid.uuid4().hex[:10]))
        temporary.rename(destination)
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)
    return destination


def go_build(destination, package, environment, desktop=False):
    print("Building", package, flush=True)
    args = ["go", "build", "-mod=readonly", "-trimpath"]
    if desktop:
        args += ["-tags", "desktop,production,webkit2_41"]
    args += ["-ldflags", "-s -w", "-o", destination, package]
    destination.parent.mkdir(parents=True, exist_ok=True)
    run(args, cwd=ROOT, env={**environment, "CGO_ENABLED": "1" if desktop else "0"})


def go_notices(bundle, environment):
    goroot = Path(output(["go", "env", "GOROOT"], cwd=ROOT, env=environment).strip())
    folder = bundle / "licenses/go-standard-library"
    folder.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(goroot / "LICENSE", folder / "LICENSE")
    used = set()
    for line in output(["go", "version", "-m", bundle / "libexec/ScreenShooterPro"], env=environment).splitlines():
        fields = line.split()
        if len(fields) >= 2 and fields[0] == "dep":
            used.add(fields[1])
    content = output(["go", "list", "-m", "-json", "all"], cwd=ROOT, env=environment)
    decoder = json.JSONDecoder()
    while content.strip():
        module, index = decoder.raw_decode(content.lstrip())
        content = content.lstrip()[index:]
        if module.get("Main") or module["Path"] not in used:
            continue
        if not module.get("Dir"):
            raise RuntimeError(f"Go source missing for notices: {module['Path']}")
        directory = Path(module["Dir"])
        names = [path for path in directory.iterdir()
                 if path.is_file() and path.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "PATENTS"))]
        if not names:
            raise RuntimeError(f"Go license missing: {module['Path']}")
        folder = bundle / "licenses/go" / module["Path"].replace("/", "_")
        folder.mkdir(parents=True, exist_ok=True)
        for path in names:
            shutil.copyfile(path, folder / path.name)


def launchers(bundle):
    shutil.copyfile(ROOT / "scripts/linux/launch.sh", bundle / "ScreenShooterPro")
    (bundle / "ScreenShooterPro").chmod(0o755)
    binaries = bundle / "bin"
    binaries.mkdir(exist_ok=True)
    for name in ["bwrap", "xdg-dbus-proxy", "xclip", "wl-copy"]:
        destination = binaries / name
        system_bwrap = ('if [ -x /usr/bin/bwrap ]; then\n'
                        '  unset LD_LIBRARY_PATH\n'
                        '  exec /usr/bin/bwrap "$@"\nfi\n') if name == "bwrap" else ""
        destination.write_text('#!/bin/sh\nset -eu\nSCP_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)\n'
                               + system_bwrap + 'exec "$SCP_ROOT/runtime/usr/bin/' + name + '" "$@"\n')
        destination.chmod(0o755)
    xdg = binaries / "xdg-open"
    xdg.write_text('#!/bin/sh\nunset LD_LIBRARY_PATH GTK_PATH GIO_MODULE_DIR GDK_PIXBUF_MODULE_FILE '
                   'GDK_PIXBUF_MODULEDIR GTK_IM_MODULE_FILE GSETTINGS_SCHEMA_DIR FONTCONFIG_FILE '
                   'WEBKIT_EXEC_PATH WEBKIT_INJECTED_BUNDLE_PATH GST_PLUGIN_SYSTEM_PATH_1_0 '
                   'LIBGL_DRIVERS_PATH __EGL_VENDOR_LIBRARY_FILENAMES\n'
                   'export XDG_DATA_DIRS="${SCREENSHOOTER_HOST_DATA_DIRS:-/usr/local/share:/usr/share}"\n'
                   'exec /usr/bin/xdg-open "$@"\n')
    xdg.chmod(0o755)


def profile(bundle):
    configuration = json.loads((ROOT / "config/runtime-linux.json").read_text())
    configuration["ocr"].update(
        executable="../runtime/usr/bin/tesseract", tessdata_directory="../runtime/usr/share/tesseract-ocr/5/tessdata",
        linux_runtime_root="../runtime", read_roots=["../runtime/usr"],
        model_revision="linux-" + sha(bundle / "runtime-manifest.json")[:20])
    metadata = json.loads((ROOT / "workers/background/runtime-manifest.json").read_text())
    configuration["background_removal"] = {
        "enabled": True, "executable": "../runtime/usr/bin/python3.12",
        "script": "../workers/background/remove_background.py", "model_path": "../models/background/u2netp.onnx",
        "model_sha256": metadata["model"]["sha256"], "model_id": "u2netp", "linux_runtime_root": "../runtime",
        "read_roots": ["../runtime/usr", "../workers/background", "../models/background"],
        "memory_bytes": 1073741824, "threads": 2, "timeout_seconds": 120}
    write_json(bundle / "config/runtime-linux.json", configuration)


def source_id():
    digest = hashlib.sha256()
    for folder in ["cmd", "internal", "frontend", "workers", "config", "scripts/linux"]:
        for path in sorted((ROOT / folder).rglob("*")):
            if path.is_file() and not any(part in {"__pycache__", "wailsjs"} for part in path.parts):
                digest.update(str(path.relative_to(ROOT)).encode())
                digest.update(path.read_bytes())
    for name in ["go.mod", "go.sum"]:
        digest.update((ROOT / name).read_bytes())
    return digest.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--offline", action="store_true", help="Forbid dependency downloads; use prepared runtime/cache")
    parser.add_argument("--refresh-runtime", action="store_true", help="Explicitly rebuild the dependency snapshot")
    args = parser.parse_args()
    if sys.platform != "linux" or platform.machine() != "x86_64" or sys.version_info[:2] != (3, 12):
        raise RuntimeError("Build on Ubuntu 24.04/Mint 22 x86_64 using /usr/bin/python3 (3.12)")
    for name in ["go", "pkg-config", "readelf", "dpkg-query", "dpkg-deb", "apt-cache", "apt-get",
                 "glib-compile-schemas", "gio-querymodules"]:
        if not shutil.which(name):
            raise RuntimeError(f"Missing build tool: {name}; see docs/linux-portable.md")
    if shutil.disk_usage(ROOT).free < 6 << 30:
        raise RuntimeError("At least 6 GiB free required for staging, runtime and archives")
    config = json.loads((ROOT / "config/linux-release.json").read_text())
    owned_directory(BASE)
    owned_directory(CACHE)
    with (BASE / "build.lock").open("a+b") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        runtime = prepare_runtime(config, args.offline, args.refresh_runtime)
        stage = Path(tempfile.mkdtemp(prefix="bundle-stage-", dir=BASE))
        try:
            bundle = stage / "ScreenShooterPro"
            shutil.copytree(runtime, bundle, copy_function=os.link)
            environment = {**os.environ, "GOTOOLCHAIN": "local"}
            if args.offline:
                environment.update(GOPROXY="off", GOSUMDB="off")
            go_build(bundle / "libexec/ScreenShooterPro", "./cmd/skrinpolka", environment, desktop=True)
            go_build(bundle / "runtime/bin/worker-guard", "./cmd/linux-worker-guard", environment)
            go_build(stage / "portable-launcher", "./cmd/linux-portable", environment)
            native = output(["readelf", "-d", bundle / "libexec/ScreenShooterPro"])
            for name in re.findall(r"\(NEEDED\).*?\[(.*?)\]", native):
                if not BASE_GLIBC.match(name) and not (bundle / "runtime/lib" / name).is_file():
                    raise RuntimeError(f"GUI dependency missing: {name}; refresh/review runtime")
            launchers(bundle)
            profile(bundle)
            worker = bundle / "workers/background"
            worker.mkdir(parents=True)
            for name in ["remove_background.py", "requirements.lock", "runtime-manifest.json"]:
                shutil.copyfile(ROOT / "workers/background" / name, worker / name)
            shutil.copytree(ROOT / "licenses", bundle / "licenses", dirs_exist_ok=True)
            go_notices(bundle, environment)
            packaging_source = bundle / "source/packaging"
            packaging_source.mkdir(parents=True)
            shutil.copyfile(__file__, packaging_source / "package.py")
            for name in ["LICENSE", "THIRD-PARTY.md"]:
                shutil.copyfile(ROOT / name, bundle / name)
            docs = bundle / "docs"
            docs.mkdir()
            shutil.copyfile(ROOT / "docs/user-guide.md", docs / "user-guide.md")
            shutil.copyfile(ROOT / "scripts/linux/README-portable.txt", bundle / "README.txt")
            shutil.copyfile(ROOT / "docs/linux-download-spec.txt", bundle / "SPECIFICATION.txt")
            identity = source_id()
            manifest = {"schema": "screenshorts-linux-release-v1", "version": config["version"],
                        "architecture": "x86_64", "baseline": config["baseline"], "source_sha256": identity,
                        "built_at": datetime.now(timezone.utc).isoformat(), "acceptance": "manual-pending",
                        "runtime_manifest_sha256": sha(bundle / "runtime-manifest.json"),
                        "license_audit": "manual-pending",
                        "go": output(["go", "version"], env=environment).strip(),
                        "files": [{"path": str(path.relative_to(bundle)), "bytes": path.stat().st_size, "sha256": sha(path)}
                                  for path in sorted(bundle.rglob("*")) if path.is_file()]}
            write_json(bundle / "release-manifest.json", manifest)
            archive = stage / config["archive_filename"]
            print("Compressing complete Linux folder", flush=True)
            with archive.open("wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", compresslevel=6, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w|", format=tarfile.PAX_FORMAT) as tar:
                    tar.add(bundle, arcname="ScreenShooterPro")
            executable = stage / config["executable_filename"]
            shutil.copyfile(stage / "portable-launcher", executable)
            with executable.open("ab") as stream, archive.open("rb") as payload:
                shutil.copyfileobj(payload, stream)
                stream.write(b"SCPLNX01" + struct.pack("<Q", archive.stat().st_size) + bytes.fromhex(sha(archive)))
            executable.chmod(0o755)
            distribution = owned_directory(BASE / "dist")
            specification = stage / config["specification_filename"]
            shutil.copyfile(ROOT / "docs/linux-download-spec.txt", specification)
            folder = distribution / ("ScreenShooterPro-" + identity[:12] + "-" + uuid.uuid4().hex[:6])
            bundle.rename(folder)
            for artifact in [archive, executable, specification]:
                os.replace(artifact, distribution / artifact.name)
            sums = stage / "SHA256SUMS.txt"
            sums.write_text("".join(sha(distribution / name) + "  " + name + "\n"
                                   for name in [archive.name, executable.name, specification.name]), encoding="ascii")
            os.replace(sums, distribution / sums.name)
            write_json(distribution / "latest-build.json", {"folder": folder.name, "source_sha256": identity,
                       "archive": archive.name, "executable": executable.name,
                       "specification": specification.name, "acceptance": "manual-pending"})
            print("Folder:", folder)
            for name in [archive.name, executable.name]:
                path = distribution / name
                print(f"Ready: {path} ({path.stat().st_size / (1024*1024):.1f} MiB)")
            print("Application, OCR, inference and tests were NOT started. Manual acceptance is pending.")
        finally:
            shutil.rmtree(stage)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError, OSError) as error:
        print(f"Linux bundle build failed: {error}", file=sys.stderr)
        sys.exit(1)
