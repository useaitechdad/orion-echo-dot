#!/usr/bin/env python3
"""Convert prepared public TRAINING features to the adaptation preservation set.

Only features/{positive,hard_negative,environment,speech}/training are read.
Never pass personal features here. Validation/testing splits are never read.
"""
import argparse
from pathlib import Path

import numpy as np
from mmap_ninja.ragged import RaggedMmap


def windows(dataset, count, positive, rng):
    if len(dataset) < count:
        raise ValueError(f'need {count} distinct clips; found {len(dataset)}')
    result = []
    for index in rng.choice(len(dataset), count, replace=False):
        matrix = np.asarray(dataset[int(index)])
        if matrix.dtype != np.uint16 or matrix.ndim != 2 or matrix.shape[1] != 40 or len(matrix) < 220:
            raise ValueError('expected uint16 frontend features with >=220 rows and 40 channels')
        # Positive speech is placed near the end by prepare_features.py.
        start = len(matrix) - 220 if positive else int(rng.integers(len(matrix) - 219))
        result.append(matrix[start:start+220])
    return np.stack(result)


def build(features, positive_count, negative_count, seed):
    if positive_count < 100 or negative_count < 1000:
        raise ValueError('adaptation needs at least 100 positive and 1000 negative clips')
    rng = np.random.default_rng(seed)
    xs, ys = [], []
    counts = [positive_count] + [negative_count // 3 + (i < negative_count % 3) for i in range(3)]
    for kind, count in zip(('positive', 'hard_negative', 'environment', 'speech'), counts):
        directory = features / kind / 'training'
        if not (directory / 'complete.json').is_file():
            raise ValueError(f'incomplete training features: {kind}')
        dataset = RaggedMmap(str(directory / 'clips_mmap'), mode='r')
        xs.append(windows(dataset, count, kind == 'positive', rng))
        ys.append(np.full(count, int(kind == 'positive'), dtype=np.uint8))
    return np.concatenate(xs), np.concatenate(ys)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--features', required=True, type=Path)
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--positives', type=int, default=1000)
    p.add_argument('--negatives', type=int, default=3000)
    p.add_argument('--seed', type=int, default=6111)
    args = p.parse_args()
    if args.output.exists():
        p.error('output exists; select a new file')
    x, y = build(args.features, args.positives, args.negatives, args.seed)
    # File handle avoids numpy silently appending .npz; exclusive create protects prior data.
    with args.output.open('xb') as f:
        np.savez_compressed(f, x=x, y=y)
    print(f'wrote {len(y)} training windows, shape={x.shape}, dtype={x.dtype}')


if __name__ == '__main__':
    main()
