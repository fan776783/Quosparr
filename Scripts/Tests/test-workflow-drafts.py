#!/usr/bin/env python3
"""Exercise release commands offline; never call the real GitHub CLI."""
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

WORKFLOWS = pathlib.Path(__file__).resolve().parents[2] / ".github/workflows"


def read_workflow(path):
    # These checks run on macOS, using its bundled Ruby/Psych YAML parser.
    result = subprocess.check_output([
        "ruby", "-rjson", "-ryaml", "-e",
        "puts JSON.generate(YAML.safe_load(File.read(ARGV.fetch(0))))", str(path)
    ], text=True)
    return json.loads(result)


class DraftWorkflowTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflows = {p.name: read_workflow(p) for p in WORKFLOWS.glob("*.yml")}

    def release_script(self, job):
        steps = self.workflows["release.yml"]["jobs"][job]["steps"]
        commands = [step["run"] for step in steps if "gh release create" in step.get("run", "")]
        self.assertEqual(len(commands), 1, "expected one active draft-creation step")
        return commands[0]

    def invoke(self, job, tag, channel, exit_code=0):
        with tempfile.TemporaryDirectory(prefix="quosparr-draft-test-") as temporary:
            root = pathlib.Path(temporary)
            capture = root / "calls.jsonl"
            recorder = root / "gh"
            recorder.write_text(
                "#!/usr/bin/env python3\nimport json, os, sys\n"
                "with open(os.environ['GH_CAPTURE'], 'a') as log:\n"
                "    log.write(json.dumps(sys.argv[1:]) + '\\n')\n"
                "sys.exit(int(os.environ['GH_CAPTURE_EXIT']))\n")
            recorder.chmod(0o755)
            (root / "dist-v1").mkdir()
            (root / "dist-v1/payload.tar.gz").write_bytes(b"command-fixture")
            (root / "release-notes.md").write_text("Draft fixture\n")
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                       GH_CAPTURE=str(capture), GH_CAPTURE_EXIT=str(exit_code),
                       GH_TOKEN="offline-command-recorder", GITHUB_REF_NAME=tag,
                       GITHUB_REPOSITORY="fixture/quosparr", GITHUB_SHA="0" * 40,
                       V1_UPDATE_CHANNEL=channel, BASH_ENV="/dev/null")
            result = subprocess.run(["bash", "--noprofile", "--norc", "-euo", "pipefail", "-c", self.release_script(job)],
                                    cwd=root, env=env, capture_output=True, text=True, timeout=10)
            calls = [json.loads(line) for line in capture.read_text().splitlines()]
            return result, calls

    def test_stable_and_beta_only_create_tagged_drafts(self):
        for job, tag, channel in (("v1-release", "v1.0.0", "stable"),
                                  ("v1-release", "beta-v1.0.0-beta.1", "beta")):
            with self.subTest(job=job, channel=channel):
                result, calls = self.invoke(job, tag, channel)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(len(calls), 1, "creating the draft is a single gh call")
                args = calls[0]
                self.assertEqual(args[:3], ["release", "create", tag])
                for flag in ("--draft", "--latest=false", "--verify-tag", "--notes-file"):
                    self.assertIn(flag, args)
                self.assertEqual("--prerelease" in args, channel == "beta")
                self.assertTrue(any(arg.endswith((".zip", ".tar.gz")) for arg in args), "draft lost its assets")

    def test_draft_creation_failure_stops_the_job(self):
        for job, channel in (("v1-release", "stable"), ("v1-release", "beta")):
            with self.subTest(job=job, channel=channel):
                result, calls = self.invoke(job, "fixture-tag", channel, exit_code=23)
                self.assertEqual(result.returncode, 23)
                self.assertEqual(len(calls), 1)

    def test_only_stable_tags_publish_as_latest(self):
        publish = []
        for name, workflow in self.workflows.items():
            for job_id, job in workflow["jobs"].items():
                for step in job.get("steps", []):
                    if "gh release edit" in step.get("run", ""):
                        publish.append((name, job_id, step))
        self.assertEqual(len(publish), 1, "expected one active publication step")
        name, job_id, step = publish[0]
        self.assertEqual((name, job_id), ("release.yml", "v1-release"))
        self.assertEqual(step["if"], "needs.v1-preflight.outputs.channel == 'stable'")
        steps = self.workflows[name]["jobs"][job_id]["steps"]
        self.assertGreater(steps.index(step), next(i for i, s in enumerate(steps) if "gh release create" in s.get("run", "")))
        self.assertEqual(step["run"], 'gh release edit "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" --draft=false --latest=true')

    def test_beta_channel_and_distribution_steps_are_inactive(self):
        forbidden = ("gh release create v1-beta", "generate-v1-homebrew.py")
        for name, workflow in self.workflows.items():
            for job_id, job in workflow["jobs"].items():
                for step in job.get("steps", []):
                    for command in forbidden:
                        self.assertNotIn(command, step.get("run", ""))
                    if (name, job_id) != ("fork-sync.yml", "windows-release"):
                        self.assertNotIn("gh release upload", step.get("run", ""))

    def test_only_publishing_jobs_have_repository_write_permission(self):
        allowed = {("release.yml", "v1-release"), ("fork-sync.yml", "sync"),
                   ("fork-sync.yml", "windows-release")}
        for name, workflow in self.workflows.items():
            self.assertEqual(workflow["permissions"], {"contents": "read"})
            for job_id, job in workflow["jobs"].items():
                write = job.get("permissions", {}).get("contents") == "write"
                self.assertEqual(write, (name, job_id) in allowed, (name, job_id))
        for job_id in ("sync", "windows-release"):
            self.assertEqual(self.workflows["fork-sync.yml"]["jobs"][job_id]["if"],
                             "github.repository == 'fan776783/Quosparr'")

    def test_fork_release_runs_ci_checks_even_without_a_merge(self):
        ci = self.workflows["ci.yml"]["jobs"]["go-checks"]["steps"]
        steps = self.workflows["fork-sync.yml"]["jobs"]["sync"]["steps"]
        checks = next(s for s in steps if s.get("run") == "bash Scripts/check-go-release.sh")
        self.assertTrue(any(s.get("run") == checks["run"] for s in ci))
        self.assertEqual(checks["if"],
                         "steps.merge.outputs.changed == 'true' || steps.merge.outputs.release == 'true'")
        publish = next(s for s in steps if s.get("name") == "Publish signed release")
        self.assertLess(steps.index(checks), steps.index(publish))
        self.assertNotIn("continue-on-error", checks)
        self.assertEqual(publish["if"], "steps.merge.outputs.release == 'true'")

    def invoke_windows_release(self, step_name, asset_names=(), *, gh_exit=0, go_exit=0, **release_fields):
        version = "1.8.2-cursor.1"
        release = dict(tagName="v" + version, isDraft=False, isPrerelease=False,
                       assets=[dict(name=name, size=100) for name in asset_names])
        release.update(release_fields)
        steps = self.workflows["fork-sync.yml"]["jobs"]["windows-release"]["steps"]
        script = next(s["run"] for s in steps if s.get("name") == step_name)
        with tempfile.TemporaryDirectory(prefix="quosparr-windows-release-test-") as temporary:
            root = pathlib.Path(temporary)
            capture, output = root / "calls.jsonl", root / "output"
            (root / "python").symlink_to(sys.executable)
            for program in ("gh", "go"):
                recorder = root / program
                recorder.write_text(
                    "#!/usr/bin/env python3\nimport json, os, sys\n"
                    f"program = {program!r}\n"
                    "with open(os.environ['GH_CAPTURE'], 'a') as log:\n"
                    "    log.write(json.dumps([program] + sys.argv[1:]) + '\\n')\n"
                    "if program == 'gh' and sys.argv[1:3] == ['release', 'view']:\n"
                    "    print(os.environ['GH_RELEASE_FIXTURE'])\n"
                    "sys.exit(int(os.environ['GO_CAPTURE_EXIT' if program == 'go' else 'GH_CAPTURE_EXIT']))\n")
                recorder.chmod(0o755)
            stage = root / "build/fork-windows/windows-amd64"
            stage.mkdir(parents=True)
            for name in (f"Quosparr Setup {version} amd64.exe",
                         f"quosparr-{version}-windows-amd64.tar.gz", "update-windows-amd64.json"):
                (stage / name).write_text("offline command fixture")
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                       FORK_VERSION=version, GH_RELEASE_FIXTURE=json.dumps(release),
                       GH_CAPTURE=str(capture), GH_CAPTURE_EXIT=str(gh_exit), GO_CAPTURE_EXIT=str(go_exit),
                       RUNNER_TEMP=str(root), GITHUB_OUTPUT=str(output), GITHUB_STEP_SUMMARY=str(root / "summary"),
                       MSYS2_ENV_CONV_EXCL=self.workflows["fork-sync.yml"]["jobs"]["windows-release"]["env"]["MSYS2_ENV_CONV_EXCL"],
                       GH_TOKEN="offline-command-recorder", GITHUB_REPOSITORY="fan776783/Quosparr", BASH_ENV="/dev/null")
            result = subprocess.run(["bash", "--noprofile", "--norc", "-euo", "pipefail", "-c", script],
                                    cwd=root, env=env, capture_output=True, text=True, timeout=10)
            calls = [json.loads(line) for line in capture.read_text().splitlines()] if capture.exists() else []
            return result, calls, output.read_text() if output.exists() else ""

    def test_windows_release_backfills_existing_release_and_skips_complete_assets(self):
        expected = ["Quosparr.Setup.1.8.2-cursor.1.amd64.exe",
                    "quosparr-1.8.2-cursor.1-windows-amd64.tar.gz", "update-windows-amd64.json"]
        for assets, output in ((["Quosparr-macos-arm64.dmg"], "needed=true\n"),
                               (expected, "needed=false\n")):
            with self.subTest(assets=assets):
                result, calls, actual = self.invoke_windows_release("Check Windows release assets", assets)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(actual, output)
                self.assertEqual(len(calls), 1)
        for fields in (dict(assets=[dict(name=expected[0], size=100)]),
                       dict(assets=[dict(name=name, size=0) for name in expected]),
                       dict(isDraft=True), dict(isPrerelease=True), dict(tagName="v1.8.0-cursor.1")):
            with self.subTest(fields=fields):
                result, calls, output = self.invoke_windows_release("Check Windows release assets", **fields)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(output, "")
        result, _, output = self.invoke_windows_release("Check Windows release assets", gh_exit=23)
        self.assertEqual(result.returncode, 23)
        self.assertEqual(output, "")

    def test_windows_only_dispatch_does_not_merge_or_publish_macos(self):
        steps = self.workflows["fork-sync.yml"]["jobs"]["sync"]["steps"]
        script = next(s["run"] for s in steps if s.get("id") == "merge")
        with tempfile.TemporaryDirectory(prefix="quosparr-windows-only-") as temporary:
            root = pathlib.Path(temporary)
            (root / "mygo.json").write_text(json.dumps(dict(version="1.8.2-cursor.1")))
            git = root / "git"
            git.write_text("#!/bin/sh\nexit 97\n")
            git.chmod(0o755)
            output = root / "output"
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"], WINDOWS_ONLY="true",
                       GITHUB_OUTPUT=str(output), GITHUB_STEP_SUMMARY=str(root / "summary"), BASH_ENV="/dev/null")
            result = subprocess.run(["bash", "--noprofile", "--norc", "-euo", "pipefail", "-c", script],
                                    cwd=root, env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output.read_text(), "version=1.8.2-cursor.1\nchanged=false\nrelease=false\n")

    def test_windows_upload_verifies_before_publishing_manifest_last(self):
        result, calls, _ = self.invoke_windows_release("Upload Windows release assets")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 3)
        self.assertEqual(calls[0][:4], ["go", "run", "./Scripts/release-assets", "verify"])
        for call in calls[1:]:
            self.assertEqual(call[:4], ["gh", "release", "upload", "v1.8.2-cursor.1"])
            self.assertNotIn("--clobber", call)
        self.assertEqual([pathlib.Path(p).name for p in calls[1][-2:]],
                         ["Quosparr.Setup.1.8.2-cursor.1.amd64.exe", "quosparr-1.8.2-cursor.1-windows-amd64.tar.gz"])
        self.assertEqual(pathlib.Path(calls[2][-1]).name, "update-windows-amd64.json")
        for gh_exit, go_exit, count in ((23, 0, 2), (0, 17, 1)):
            result, calls, _ = self.invoke_windows_release("Upload Windows release assets", gh_exit=gh_exit, go_exit=go_exit)
            self.assertEqual(result.returncode, gh_exit or go_exit)
            self.assertEqual(len(calls), count, "failure must not expose the update manifest")

    def test_windows_signing_keys_are_excluded_from_msys_path_conversion(self):
        job = self.workflows["fork-sync.yml"]["jobs"]["windows-release"]
        self.assertEqual(set(job["env"]["MSYS2_ENV_CONV_EXCL"].split(";")),
                         {"MYGO_UPDATE_PRIVATE_KEY", "MYGO_UPDATER_PRIVATE_KEY"})
        result, calls, _ = self.invoke_windows_release("Verify signing key transport")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, [])
        self.assertIn("transport verified", result.stdout)

    def test_windows_release_builds_tag_and_requires_native_verification(self):
        jobs = self.workflows["fork-sync.yml"]["jobs"]
        job = jobs["windows-release"]
        self.assertEqual(job["needs"], "sync")
        self.assertEqual(job["runs-on"], "windows-latest")
        self.assertEqual(jobs["sync"]["outputs"]["version"], "${{ steps.merge.outputs.version }}")
        steps = job["steps"]
        checkout = next(s for s in steps if s.get("uses", "").startswith("actions/checkout@"))
        self.assertEqual(checkout["with"]["ref"], "refs/tags/v${{ needs.sync.outputs.version }}")
        package = next(s for s in steps if s.get("name") == "Package signed Windows x64")
        self.assertEqual(package["env"]["V1_DEVELOPMENT_PACKAGE"], "0")
        self.assertEqual(package["env"]["MYGO_UPDATE_PRIVATE_KEY"], "${{ secrets.FORK_UPDATE_PRIVATE_KEY }}")
        native = next(s for s in steps if s.get("name") == "Verify native Windows installation")
        upload = next(s for s in steps if s.get("name") == "Upload Windows release assets")
        self.assertLess(steps.index(package), steps.index(native))
        self.assertLess(steps.index(native), steps.index(upload))
        self.assertIn("verify-v1-windows-native.ps1", native["run"])
        for step in steps[1:]:
            self.assertEqual(step["if"], "steps.assets.outputs.needed == 'true'")
            self.assertNotIn("continue-on-error", step)


if __name__ == "__main__":
    unittest.main()
