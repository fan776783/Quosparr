#!/usr/bin/env python3
"""Exercise fork release staging and signing preflight without real credentials."""
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]


class ForkReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="quosparr-fork-release-test-")
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        (self.root / "Scripts").mkdir()
        for name in ("package-v1-app.sh", "codesign-helpers.sh", "stage-v1-config.py", "fork_release_config.py"):
            shutil.copyfile(ROOT / "Scripts" / name, self.root / "Scripts" / name)
        self.config = json.loads((ROOT / "mygo.json").read_text())
        self.config["version"] = "1.11.2-cursor.1"
        self.env = dict(os.environ, V1_UPDATE_CHANNEL="stable", V1_DEVELOPMENT_PACKAGE="0",
                        V1_CONFIG_PATH=str(self.root / "mygo.json"), BASH_ENV="/dev/null",
                        V1_BUILD_DIR=str(self.root / "build"), GOCACHE=str(self.root / "cache"),
                        FORK_ADHOC_PACKAGE="0", MYGO_UPDATE_PRIVATE_KEY="synthetic-test-key")
        for name in ("MACOS_DEV_IDENTITY", "APPLE_API_KEY_PATH", "APPLE_API_KEY_ID",
                     "APPLE_API_ISSUER_ID", "MYGO_UPDATER_PRIVATE_KEY"):
            self.env.pop(name, None)

    def write_config(self):
        (self.root / "mygo.json").write_text(json.dumps(self.config))

    def stage(self):
        self.write_config()
        result = subprocess.run([sys.executable, str(self.root / "Scripts/stage-v1-config.py"),
                                 str(self.root), str(self.root / "staged")],
                                env=self.env, text=True, capture_output=True, timeout=10)
        return result

    def package(self):
        self.write_config()
        # Stop at the build call: this exercises real config/signing preflight
        # but cannot build an app, invoke codesign, or use the user's keychain.
        binary = self.root / "go"
        binary.write_text(f"#!{sys.executable}\nimport pathlib, sys\n"
                          "if sys.argv[1] == 'list':\n    print('v0.7.4')\n"
                          "else:\n    pathlib.Path('build-reached').touch()\n    sys.exit(42)\n")
        binary.chmod(0o755)
        self.env["PATH"] = str(self.root) + os.pathsep + os.environ["PATH"]
        return subprocess.run([shutil.which("bash"), str(self.root / "Scripts/package-v1-app.sh")],
                              env=self.env, cwd=self.root, text=True, capture_output=True, timeout=10)

    def test_fork_stable_keeps_version_repository_and_public_key(self):
        result = self.stage()
        self.assertEqual(result.returncode, 0, result.stderr)
        staged = json.loads((self.root / "staged/mygo.json").read_text())
        self.assertEqual(staged["version"], self.config["version"])
        self.assertEqual(staged["updates"], dict(self.config["updates"], tagPrefix="v"))

    def test_stable_rejects_beta_invalid_fork_and_mismatched_repository(self):
        for version, repository, url in (
            ("1.12.0-beta.1", "Licoy/Quosparr", ""),
            ("1.11.2-cursor.0", "fan776783/Quosparr", ""),
            ("1.11.2-cursor.1", "Licoy/Quosparr", ""),
            ("1.11.2-cursor.1", "fan776783/Quosparr", "https://updates.example.invalid"),
        ):
            with self.subTest(version=version, repository=repository, url=url):
                self.config["version"] = version
                self.config["updates"].update(github=repository, url=url)
                self.assertNotEqual(self.stage().returncode, 0)

    def test_fork_cannot_switch_to_upstream_beta_but_dev_disables_updates(self):
        self.env["V1_UPDATE_CHANNEL"] = "beta"
        self.assertNotEqual(self.stage().returncode, 0)
        self.env["V1_DEVELOPMENT_PACKAGE"] = "1"
        self.assertEqual(self.stage().returncode, 0)
        self.assertNotIn("updates", json.loads((self.root / "staged/mygo.json").read_text()))

    def test_explicit_fork_adhoc_release_reaches_build_without_apple_credentials(self):
        self.env["FORK_ADHOC_PACKAGE"] = "1"
        result = self.package()
        self.assertEqual(result.returncode, 42, result.stderr)
        self.assertTrue((self.root / "build-reached").exists())

    def test_default_release_still_requires_apple_signing(self):
        self.config["version"] = "1.11.2"
        self.config["updates"]["github"] = "Licoy/Quosparr"
        result = self.package()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Missing MACOS_DEV_IDENTITY", result.stderr)
        self.assertFalse((self.root / "build-reached").exists())

    def test_adhoc_mode_cannot_publish_upstream_config(self):
        self.env["FORK_ADHOC_PACKAGE"] = "1"
        self.config["updates"]["github"] = "Licoy/Quosparr"
        result = self.package()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires the fork's version and update repository", result.stderr)
        self.assertFalse((self.root / "build-reached").exists())

    def test_adhoc_release_still_requires_update_signature(self):
        self.env.update(FORK_ADHOC_PACKAGE="1", MYGO_UPDATE_PRIVATE_KEY="")
        result = self.package()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Missing MYGO_UPDATE_PRIVATE_KEY", result.stderr)
        self.assertFalse((self.root / "build-reached").exists())

    def test_windows_archive_preserves_other_powershell_resources(self):
        self.write_config()
        stage = self.root / "windows"
        stage.mkdir()
        files = {"Quosparr.exe": b"application-with-embedded-sqlite", "LICENSE": b"license",
                 "support.ps1": b"application-resource"}
        for name, data in files.items():
            (stage / name).write_bytes(data)
        (stage / "install.ps1").write_bytes(b"download-helper-not-installed")
        result = subprocess.run([sys.executable, str(ROOT / "Scripts/verify-v1-platform.py"),
                                 "archive-windows", str(self.root / "mygo.json"), str(stage), "windows/amd64"],
                                env=self.env, text=True, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        with tarfile.open(stage / f"quosparr-{self.config['version']}-windows-amd64.tar.gz", "r:gz") as archive:
            archived = {member.name: archive.extractfile(member).read() for member in archive}
        self.assertEqual(archived, files)


if __name__ == "__main__":
    unittest.main()
