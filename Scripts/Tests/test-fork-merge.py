#!/usr/bin/env python3
"""Exercise the real merge helper against divergent temporary Git histories."""
import json
import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]


class ForkMergeTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = pathlib.Path(self.tmp.name)
        self.git('init', '-b', 'main')
        self.git('config', 'user.name', 'Test')
        self.git('config', 'user.email', 'test@example.invalid')
        (self.repo / 'Scripts').mkdir()
        for name in ('fork-merge.sh', 'fork-sync-config.py'):
            shutil.copyfile(ROOT / 'Scripts' / name, self.repo / 'Scripts' / name)
        self.config('1.8.2', 'upstream')
        (self.repo / 'source.go').write_text('base\n')
        self.commit()
        self.git('branch', 'upstream')
        self.config('1.8.2-cursor.2', 'fork')
        self.commit()
        self.before = self.git('rev-parse', 'HEAD').strip()

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.repo, text=True, stderr=subprocess.STDOUT)

    def config(self, version, identity):
        (self.repo / 'mygo.json').write_text(json.dumps({
            'version': version, 'updates': {'github': identity, 'publicKey': identity},
            'upstreamField': version,
        }, indent=2) + '\n')

    def commit(self):
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')

    def merge(self):
        return subprocess.run(['bash', 'Scripts/fork-merge.sh', 'upstream'],
                              cwd=self.repo, text=True, capture_output=True)

    def test_version_conflict_preserves_identity_and_upstream_fields(self):
        self.git('checkout', 'upstream')
        self.config('1.9.1', 'upstream')
        self.commit()
        self.git('checkout', 'main')
        result = self.merge()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        data = json.loads((self.repo / 'mygo.json').read_text())
        self.assertEqual(data['version'], '1.9.1-cursor.1')
        self.assertEqual(data['updates'], {'github': 'fork', 'publicKey': 'fork'})
        self.assertEqual(data['upstreamField'], '1.9.1')
        self.assertEqual(len(self.git('rev-list', '--parents', '-n', '1', 'HEAD').split()), 3)
        head = self.git('rev-parse', 'HEAD')
        self.assertEqual(self.merge().returncode, 0)
        self.assertEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertEqual(self.git('status', '--porcelain'), '')

    def test_source_conflict_aborts_without_discarding_fork(self):
        (self.repo / 'source.go').write_text('fork\n')
        self.commit()
        head = self.git('rev-parse', 'HEAD')
        self.git('checkout', 'upstream')
        (self.repo / 'source.go').write_text('upstream\n')
        self.commit()
        self.git('checkout', 'main')
        result = self.merge()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('source.go', result.stdout)
        self.assertEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertEqual(self.git('status', '--porcelain'), '')
        self.assertEqual((self.repo / 'source.go').read_text(), 'fork\n')

    def test_same_version_keeps_fork_revision(self):
        self.git('checkout', 'upstream')
        (self.repo / 'source.go').write_text('upstream\n')
        self.commit()
        self.git('checkout', 'main')
        self.assertEqual(self.merge().returncode, 0)
        self.assertEqual(json.loads((self.repo / 'mygo.json').read_text())['version'], '1.8.2-cursor.2')

    def test_prerelease_aborts_merge(self):
        self.git('checkout', 'upstream')
        self.config('1.9.1-beta.1', 'upstream')
        self.commit()
        self.git('checkout', 'main')
        self.assertNotEqual(self.merge().returncode, 0)
        self.assertEqual(self.git('rev-parse', 'HEAD').strip(), self.before)
        self.assertEqual(self.git('status', '--porcelain'), '')

    def test_dirty_tree_is_preserved(self):
        (self.repo / 'source.go').write_text('work in progress\n')
        self.assertNotEqual(self.merge().returncode, 0)
        self.assertEqual((self.repo / 'source.go').read_text(), 'work in progress\n')


if __name__ == '__main__':
    unittest.main()
