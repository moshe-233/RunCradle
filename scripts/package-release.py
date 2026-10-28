#!/usr/bin/env python3
"""Build an offline Linux amd64 directory bundle. Python standard library only."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def validate_version(version):
    number = r"(?:0|[1-9][0-9]*)"
    identifier = r"[0-9A-Za-z-]+"
    pattern = rf"{number}\.{number}\.{number}(?:-({identifier}(?:\.{identifier})*))?(?:\+{identifier}(?:\.{identifier})*)?"
    match = re.fullmatch(pattern, version, flags=re.ASCII)
    if not match or (match[1] and any(x.isdigit() and len(x) > 1 and x[0] == "0" for x in match[1].split("."))):
        raise ValueError("--version must be a safe SemVer, for example 0.1.0-dev")


def source_identity(root, allow_dirty):
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, timeout=5, text=True).strip()
    dirty = bool(subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=all"], cwd=root, timeout=5))
    if dirty and not allow_dirty:
        raise ValueError("source tree is dirty; commit it or explicitly pass --allow-dirty for a local candidate")
    return commit, dirty


def validate_elf(path):
    with path.open("rb") as stream:
        header = stream.read(64)
        if len(header) != 64 or header[:7] != b"\x7fELF\x02\x01\x01" or header[7] not in (0, 3):
            raise ValueError(f"not a Linux amd64 ELF: {path}")
        if struct.unpack_from("<H", header, 18)[0] != 62 or struct.unpack_from("<H", header, 16)[0] not in (2, 3):
            raise ValueError(f"wrong ELF architecture/type: {path}")
        offset = struct.unpack_from("<Q", header, 32)[0]
        size, count = struct.unpack_from("<HH", header, 54)
        if size != 56 or count == 0 or offset + size * count > path.stat().st_size:
            raise ValueError(f"invalid ELF program headers: {path}")
        stream.seek(offset)
        for _ in range(count):
            entry = stream.read(size)
            if struct.unpack_from("<I", entry)[0] in (2, 3):
                raise ValueError(f"ELF requires a dynamic loader/libraries: {path}")


def validate_engine(path, pin):
    if not path.is_file() or path.stat().st_size > 256 << 20 or sha256(path) != pin["binary_sha256"]:
        raise ValueError("engine does not match the fixed official SHA-256; run make prepare")
    validate_elf(path)


def license_files(directory):
    try:
        inventory = json.loads((directory / "inventory.json").read_text())
        required = [directory / name for name in ("LICENSE", "Go-LICENSE", "Go-PATENTS")]
        pin = json.loads((ROOT / "internal/processcompose/pinned.json").read_text())
        if inventory["engine_binary_sha256"] != pin["binary_sha256"] or not inventory["modules"]:
            raise ValueError("license inventory engine identity mismatch")
        expected = {tuple(line.split()[1:4]) for line in inventory["binary_buildinfo"].splitlines() if line.startswith("\tdep\t")}
        observed = {(m["module"], m["version"], m["module_sum"]) for m in inventory["modules"]}
        if not expected or observed != expected or len(observed) != len(inventory["modules"]):
            raise ValueError("license inventory omits or duplicates a binary dependency")
        for module in inventory["modules"]:
            if not module["files"]:
                raise ValueError("module has no license material")
            for entry in module["files"]:
                rel = Path(entry["path"])
                if rel.is_absolute() or ".." in rel.parts:
                    raise ValueError("unsafe license inventory path")
                path = directory / rel
                if not path.is_file() or path.is_symlink() or sha256(path) != entry["sha256"]:
                    raise ValueError(f"missing or changed license material: {rel}")
        if any(not p.is_file() or p.stat().st_size == 0 for p in required):
            raise ValueError("missing primary or Go runtime license")
        return sorted(p for p in directory.rglob("*") if p.is_file())
    except (OSError, KeyError, json.JSONDecodeError) as error:
        raise ValueError(f"missing license material/inventory: {error}") from error


def file_manifest(root):
    files = []
    for path in sorted(root.rglob("*")):
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise ValueError(f"bundle cannot contain links or special files: {path}")
        if path.is_file() and path != root / "bundle.json":
            files.append({"path": path.relative_to(root).as_posix(), "sha256": sha256(path)})
    return files


def write_archive(stage, output):
    # Exclusive creation avoids both accidental replacement and check/open races.
    file_manifest(stage)
    with output.open("xb") as stream:
        try:
            with gzip.GzipFile(filename="", mode="wb", fileobj=stream, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                    for path in [stage, *sorted(stage.rglob("*"))]:
                        name = path.relative_to(stage.parent).as_posix()
                        info = archive.gettarinfo(str(path), arcname=name)
                        relative = path.relative_to(stage).as_posix()
                        info.mode = 0o755 if path.is_dir() or relative in ("runcradle", "libexec/process-compose") else 0o644
                        info.uid = info.gid = info.mtime = 0
                        info.uname = info.gname = ""
                        if path.is_file():
                            with path.open("rb") as source:
                                archive.addfile(info, source)
                        else:
                            archive.addfile(info)
        except BaseException:
            output.unlink()
            raise


def publish_checksum(archive, checksum):
    # archive was created exclusively by this invocation. Never unlink a sidecar
    # whose exclusive creation failed, as it belongs to an earlier producer.
    created = False
    try:
        with checksum.open("x") as stream:
            created = True
            stream.write(f"{sha256(archive)}  {archive.name}\n")
    except BaseException:
        if created:
            checksum.unlink()
        archive.unlink()
        raise


def build_environment():
    return dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GONOPROXY="none", GOVCS="*:off", GOSUMDB="off", CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")


def package(version, engine, out, allow_dirty):
    validate_version(version)
    commit, dirty = source_identity(ROOT, allow_dirty)
    pin = json.loads((ROOT / "internal/processcompose/pinned.json").read_text())
    validate_engine(engine, pin)
    material = ROOT / "third_party/licenses/process-compose"
    license_files(material)
    out.mkdir(parents=True, exist_ok=True)
    name = f"runcradle-{version}-linux-amd64"
    archive = out / f"{name}.tar.gz"
    checksum = out / f"{name}.tar.gz.sha256"
    if archive.exists() or archive.is_symlink() or checksum.exists() or checksum.is_symlink():
        raise FileExistsError("refusing to overwrite an existing release or checksum")
    with tempfile.TemporaryDirectory(prefix=".package-", dir=out) as temporary:
        stage = Path(temporary) / name
        (stage / "libexec").mkdir(parents=True)
        environment = build_environment()
        for key, default in (("GOCACHE", ROOT / ".cache/go-build"), ("GOPATH", ROOT / ".cache/go"), ("GOMODCACHE", ROOT / ".cache/go-mod"), ("TMPDIR", ROOT / ".work/tmp")):
            environment.setdefault(key, str(default))
            Path(environment[key]).mkdir(parents=True, exist_ok=True)
        flags = f"-X runcradle/internal/buildinfo.Version={version} -X runcradle/internal/buildinfo.Commit={commit} -X runcradle/internal/buildinfo.Distribution=bundle"
        subprocess.run([environment.get("GO", "go"), "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-ldflags", flags, "-o", str(stage / "runcradle"), "./cmd/runcradle"], cwd=ROOT, env=environment, check=True, timeout=300)
        validate_elf(stage / "runcradle")
        shutil.copyfile(engine, stage / "libexec/process-compose")
        shutil.copyfile(ROOT / "LICENSE", stage / "LICENSE")
        (stage / "THIRD_PARTY_NOTICES.md").write_text((ROOT / "THIRD_PARTY_NOTICES.md").read_text().replace("](third_party/licenses/", "](licenses/"))
        shutil.copyfile(ROOT / "packaging/README.md", stage / "README.md")
        shutil.copytree(ROOT / "third_party/licenses", stage / "licenses")
        (stage / "examples").mkdir()
        shutil.copyfile(ROOT / "examples/bundled-hello.yaml", stage / "examples/hello.yaml")
        manifest = {"schema_version": 1, "package_version": version, "source_commit": commit, "source_dirty": dirty, "os": "linux", "arch": "amd64", "engine": {"path": "libexec/process-compose", "version": pin["version"], "sha256": pin["binary_sha256"]}, "files": file_manifest(stage)}
        raw = (json.dumps(manifest, indent=2) + "\n").encode()
        if len(raw) > 64 << 10:
            raise ValueError("bundle.json exceeds 64 KiB")
        (stage / "bundle.json").write_bytes(raw)
        write_archive(stage, archive)
        publish_checksum(archive, checksum)
    return archive


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    cache = Path(os.environ.get("RUNCRADLE_CACHE_DIR", ROOT / ".cache"))
    parser.add_argument("--engine", type=Path, default=cache / "process-compose/v1.122.0/process-compose")
    parser.add_argument("--out", type=Path, default=ROOT / ".work/dist")
    parser.add_argument("--allow-dirty", action="store_true")
    args = parser.parse_args()
    try:
        print(package(args.version, args.engine.resolve(), args.out.resolve(), args.allow_dirty))
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(2, f"package-release: {error}\n")


if __name__ == "__main__":
    main()
