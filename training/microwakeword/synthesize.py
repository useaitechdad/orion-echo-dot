#!/usr/bin/env python3
"""Generate public/synthetic-only data with disjoint speaker pools.

Run inside the dedicated synthesis environment. No personal audio is read.
Upstream piper-sample-generator: 2971426a55072f7d22fec416ca7800df8bd23207.
"""
import argparse
import itertools
import json
from pathlib import Path
import random
import sys
import types


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--workspace', type=Path, required=True)
    p.add_argument('--smoke', action='store_true')
    p.add_argument('--revision', choices=['v1', 'v2'], default='v1')
    args = p.parse_args()
    root = args.workspace.resolve()
    sys.path.insert(0, str(root / 'piper-source'))
    import torch
    import numpy as np
    import piper_sample_generator.__main__ as generator

    for split, start, stop, positives, negatives in [
        ('training', 0, 600, 6000, 1200),
        ('validation', 600, 700, 1000, 200),
        ('testing', 700, 800, 1000, 200),
    ]:
        # v1 covered one espeak pronunciation only. v2 also covers rounded /oʊ/,
        # reduced initial vowels, and weakened /r/, without adding a new phrase.
        positive_phonemes = [
            'hˈeɪ ɔɹˈaɪən.', 'hˈeɪ ɔɹˈaɪən.',
            'hˈeɪ oʊɹˈaɪən.', 'hˈeɪ oʊɹˈaɪən.',
            'hˈeɪ əɹˈaɪən.', 'hˈeɪ ɔːɹˈaɪən.',
            'hˈeɪ oʊˈaɪən.', 'hˈeɪ ɔˈaɪən.',
        ]
        for label, count, phrases in [
            ('positive', positives, ['Hey Orion.']),
            ('negative', negatives, ['Hey Ryan.', 'Hey Brian.', 'Orion.', 'Hey there.',
                                     'Hey, are you on?', 'Turn it on.', 'Hey Oliver.',
                                     'Hey Siri.', 'Hello Orion.', 'Ryan.']),
        ]:
            phoneme_input = label == 'positive' and args.revision == 'v2'
            if phoneme_input:
                phrases = positive_phonemes
            directory = 'synthetic' + ('-v2' if args.revision == 'v2' else '')
            if args.smoke:
                directory += '-smoke'
            out = root / directory / split / label
            if (out / 'complete.json').exists():
                continue
            if out.exists() and any(out.iterdir()):
                raise RuntimeError(f'Incomplete output preserved: {out}; choose a new workspace/revision')
            seed = 6100 + start + (1 if label == 'negative' else 0)
            rng = random.Random(seed)
            random.seed(seed)
            np.random.seed(seed)
            torch.manual_seed(seed)

            def product(*values):
                if len(values) == 2 and all(isinstance(v, range) for v in values):
                    # Upstream's Cartesian-order iterator starts with one speaker
                    # repeated in every blend. Sample only this split's pool.
                    def pairs():
                        while True:
                            yield rng.randrange(start, stop), rng.randrange(start, stop)
                    return pairs()
                return itertools.product(*values)

            generator.it = types.SimpleNamespace(product=product, cycle=itertools.cycle,
                                                 islice=itertools.islice)
            n = 2 if args.smoke else count
            generator.generate_samples(
                text=phrases, output_dir=out,
                model=root / 'models/en_US-libritts_r-medium.pt',
                max_samples=n, batch_size=16, max_speakers=800,
                slerp_weights=(.25, .5, .75),
                length_scales=(.8, 1., 1.2),
                noise_scales=(.5, .667, .8), noise_scale_ws=(.6, .8),
                phoneme_input=phoneme_input,
            )
            files = list(out.glob('*.wav'))
            if len(files) != n:
                raise RuntimeError(f'Expected {n} files, got {len(files)}')
            (out / 'complete.json').write_text(json.dumps({
                'split': split, 'label': label, 'count': n, 'speaker_start': start,
                'speaker_stop_exclusive': stop, 'seed': seed, 'phrases': phrases,
                'phoneme_input': phoneme_input, 'revision': args.revision,
            }, indent=2) + '\n')
            print(f'COMPLETE {split}/{label}: {n}', flush=True)


if __name__ == '__main__':
    main()
