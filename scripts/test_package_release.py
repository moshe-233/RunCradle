import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent

class PackageTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location("release", ROOT / "scripts/package-release.py")
        cls.release = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.release)

    def test_offline_build_does_not_use_private_direct_vcs(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(os.environ, {"GONOPROXY": "*", "GOPRIVATE": "*", "GOVCS": "*:off", "GOENV": "off", "GOMODCACHE": str(Path(directory) / "empty-cache")}):
                environment = self.release.build_environment()
            result = subprocess.run([environment.get("GO", "go"), "mod", "download", "-json", "github.com/gorilla/websocket@v1.5.3"], cwd=directory, env=environment, capture_output=True, text=True, timeout=15)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("module lookup disabled by GOPROXY=off", result.stdout + result.stderr)
            self.assertNotIn("GOVCS disallows", result.stdout + result.stderr)

    def test_bad_version_rejected_before_build(self):
        for version in ("", "../bad", "1.2", "1.2.3;id", "01.2.3", "1.2.3-01", "1.2.3 a"):
            with self.subTest(version=version), self.assertRaises(ValueError):
                self.release.validate_version(version)
        self.release.validate_version("1.2.3-rc.1+local")

    def test_wrong_engine_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "engine"
            path.write_text("#!/bin/sh\nexit 0\n")
            with self.assertRaises(ValueError):
                self.release.validate_engine(path, json.loads((ROOT / "internal/processcompose/pinned.json").read_text()))

    def test_missing_license_material_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                self.release.license_files(Path(directory))

    def test_dirty_source_requires_opt_in(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q", root], check=True)
            subprocess.run(["git", "-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"], check=True)
            (root / "untracked").write_text("dirty")
            with self.assertRaises(ValueError):
                self.release.source_identity(root, False)
            self.assertTrue(self.release.source_identity(root, True)[1])

    def test_license_inventory_cannot_omit_binary_dependencies(self):
        import shutil
        with tempfile.TemporaryDirectory() as directory:
            material = Path(directory) / "licenses"
            shutil.copytree(ROOT / "third_party/licenses/process-compose", material)
            path = material / "inventory.json"
            inventory = json.loads(path.read_text())
            inventory["modules"].pop()
            path.write_text(json.dumps(inventory))
            with self.assertRaises(ValueError):
                self.release.license_files(material)

    def test_archive_layout_and_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stage = root / "runcradle-1.2.3-linux-amd64"
            (stage / "libexec").mkdir(parents=True)
            (stage / "runcradle").write_text("binary")
            (stage / "libexec/process-compose").write_text("engine")
            (stage / "README.md").write_text("read me")
            archive = root / "bundle.tar.gz"
            self.release.write_archive(stage, archive)
            with tarfile.open(archive) as tar:
                for item in tar:
                    self.assertTrue(item.name.startswith(stage.name))
                    self.assertFalse(item.issym() or item.islnk())
                    want = 0o755 if item.isdir() or item.name.endswith(("/runcradle", "/libexec/process-compose")) else 0o644
                    self.assertEqual(item.mode, want)

    def test_existing_output_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stage = root / "stage"
            stage.mkdir()
            output = root / "bundle.tar.gz"
            output.write_bytes(b"keep")
            with self.assertRaises(FileExistsError):
                self.release.write_archive(stage, output)
            self.assertEqual(output.read_bytes(), b"keep")

    def test_checksum_write_failure_cleans_only_its_outputs(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "archive.tar.gz"
            checksum = Path(directory) / "archive.tar.gz.sha256"
            archive.write_bytes(b"archive")
            original_open = Path.open
            class FailingStream:
                def __init__(self, stream):
                    self.stream = stream
                def __enter__(self):
                    return self
                def write(self, text):
                    self.stream.write("partial")
                    raise OSError("injected disk full")
                def __exit__(self, *args):
                    self.stream.close()
            def open_path(path, *args, **kwargs):
                stream = original_open(path, *args, **kwargs)
                return FailingStream(stream) if path == checksum and args[0] == "x" else stream
            with patch.object(Path, "open", open_path):
                with self.assertRaises(OSError):
                    self.release.publish_checksum(archive, checksum)
            self.assertFalse(archive.exists())
            self.assertFalse(checksum.exists())
            archive.write_bytes(b"archive")
            checksum.write_bytes(b"existing")
            with self.assertRaises(FileExistsError):
                self.release.publish_checksum(archive, checksum)
            self.assertEqual(checksum.read_bytes(), b"existing")

    def test_manifest_hashes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "README.md").write_bytes(b"abc")
            (root / "bundle.json").write_text("excluded")
            self.assertEqual(self.release.file_manifest(root), [{"path": "README.md", "sha256": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"}])
            (root / "link").symlink_to("README.md")
            with self.assertRaises(ValueError):
                self.release.file_manifest(root)

if __name__ == "__main__":
    unittest.main()
