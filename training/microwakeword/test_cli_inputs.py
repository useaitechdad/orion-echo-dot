"""Fail early on missing evaluation data and ambiguous mining outputs."""
from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent


class InputTests(unittest.TestCase):
    def invoke(self, script, root, *args):
        result = subprocess.run([sys.executable, str(HERE / script), '--workspace', str(root),
            '--helper', '/unused/helper', '--model', '/unused/model', *map(str, args)],
            capture_output=True, env={**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'})
        self.assertEqual(result.returncode, 2, result.stderr)
        return result.stderr

    def test_each_evaluation_group_is_required(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for group, name in [('target', 'synthetic-v2/testing/positive/a.wav'),
                                ('confusable', 'synthetic-v2/testing/negative/a.wav'),
                                ('speech', 'speech_raw/LibriSpeech/test-clean/1/2/a.flac')]:
                error = self.invoke('evaluate_native.py', root, '--synthetic', 'synthetic-v2',
                                    '--output', root / 'report.json')
                self.assertIn(('empty evaluation group '+group).encode(), error)
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.touch()  # Discovery only; no audio is opened before all groups exist.
            meta = root / 'environmental/ESC-50-master/meta/esc50.csv'
            meta.parent.mkdir(parents=True)
            meta.write_text('filename,fold\n')
            error = self.invoke('evaluate_native.py', root, '--synthetic', 'synthetic-v2',
                                '--output', root / 'report.json')
            self.assertIn(b'empty evaluation group environment', error)
            self.assertFalse((root / 'report.json').exists())

    def test_mining_requires_exact_new_npz_and_sidecar(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            error = self.invoke('mine_local_negatives.py', root, '--output', root / 'candidate')
            self.assertIn(b'.npz suffix', error)
            marker = root / 'candidate.json'
            marker.write_text('preserve')
            error = self.invoke('mine_local_negatives.py', root, '--output', root / 'candidate.npz')
            self.assertIn(b'preserve existing output', error)
            self.assertEqual(marker.read_text(), 'preserve')
            self.assertFalse((root / 'candidate.npz').exists())


if __name__ == '__main__':
    unittest.main()
