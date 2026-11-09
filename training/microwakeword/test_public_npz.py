"""Check preservation-set scaling, split isolation and validation."""
from pathlib import Path
import tempfile
import unittest

import numpy as np
from mmap_ninja.ragged import RaggedMmap
from make_public_npz import build, windows


class PreservationTests(unittest.TestCase):
    def test_training_only_and_integer_scale(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for kind in ('positive', 'hard_negative', 'environment', 'speech'):
                folder = root / kind / 'training'
                folder.mkdir(parents=True)
                (folder / 'complete.json').write_text('{}')
                def clips():
                    for _ in range(340):
                        yield np.tile(np.arange(230, dtype=np.uint16)[:, None], (1, 40))
                RaggedMmap.from_generator(out_dir=str(folder / 'clips_mmap'),
                    sample_generator=clips(), batch_size=100, verbose=False)
                # If the exporter reads held-out data, this malformed directory fails.
                (root / kind / 'testing').mkdir()
            x, y = build(root, 100, 1000, 1)
            self.assertEqual(x.shape, (1100, 220, 40))
            self.assertEqual(x.dtype, np.uint16)
            self.assertEqual(int(y.sum()), 100)
            self.assertTrue(np.all(x[:100, 0, :] == 10))
            self.assertAlmostEqual(float(x[0, -1, 0]) * .0390625, 8.9453125)
            again, labels = build(root, 100, 1000, 1)
            np.testing.assert_array_equal(x, again)
            np.testing.assert_array_equal(y, labels)

    def test_rejects_float_and_short_features(self):
        for matrix in (np.zeros((220, 40), np.float32), np.zeros((219, 40), np.uint16)):
            with self.assertRaises(ValueError):
                windows([matrix], 1, True, np.random.default_rng(1))
        with self.assertRaises(ValueError):
            windows([], 1, True, np.random.default_rng(1))


if __name__ == '__main__':
    unittest.main()
