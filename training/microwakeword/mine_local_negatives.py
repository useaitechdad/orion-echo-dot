#!/usr/bin/env python3
"""Mine high-scoring public TRAINING negatives locally; never upload a model."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import csv
import json
import math
import os
from pathlib import Path
import struct
import subprocess

import numpy as np
from pymicro_features import MicroFrontend
from scipy.signal import resample_poly
import soundfile as sf


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--workspace', required=True, type=Path)
    p.add_argument('--helper', required=True, type=Path)
    p.add_argument('--model', required=True, type=Path)
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--revision', choices=['v1', 'v2'], default='v1')
    args = p.parse_args()
    if args.output.suffix != '.npz':
        p.error('output must have a .npz suffix')
    if args.output.exists() or args.output.with_suffix('.json').exists():
        p.error('preserve existing output; choose a new destination')
    root = args.workspace.resolve()
    paths = [x for x in (root / 'speech_raw/LibriSpeech/dev-clean').glob('*/*/*.flac')
             if int(x.parent.parent.name) % 5 != 0]
    synthetic = 'synthetic-v2' if args.revision == 'v2' else 'synthetic'
    paths += list((root / synthetic / 'training/negative').glob('*.wav'))
    esc = root / 'environmental/ESC-50-master'
    with (esc / 'meta/esc50.csv').open() as f:
        paths += [esc / 'audio' / r['filename'] for r in csv.DictReader(f) if int(r['fold']) <= 3]
    if len(paths) < 4000:
        raise ValueError('incomplete public training corpus')

    def mine(path):
        audio, sr = sf.read(path, dtype='float32')
        d = math.gcd(sr, 16000)
        audio = resample_poly(audio, 16000//d, sr//d)
        raw = np.round(np.clip(audio, -1, 1)*32767).astype('<i2').tobytes() + bytes(32000)
        framed = b''.join(struct.pack('<H', len(raw[i:i+1024]))+raw[i:i+1024]
                          for i in range(0, len(raw), 1024))
        r = subprocess.run([str(args.helper.resolve()), str(args.model.resolve()), '.5'],
            input=framed, capture_output=True, check=True,
            env={**os.environ, 'ORION_WAKE_SCORES': '1'})
        scores = np.array([float(s.split('=')[1]) for s in r.stderr.decode().splitlines()
                           if s.startswith('wake_probability=')])
        if not len(scores) or scores.max() < .1:
            return [], None
        indices = []
        for index in np.argsort(scores)[::-1]:
            if scores[index] < .1 or len(indices) == 3:
                break
            if all(abs(index-other) > 15 for other in indices):
                indices.append(int(index))
        frontend = MicroFrontend()
        rows = []
        for i in range(0, len(raw)-319, 320):
            result = frontend.process_samples(raw[i:i+320])
            if result.features:
                rows.append(result.features)
        features = np.asarray(rows, dtype=np.float32)
        windows = []
        for index in indices:
            # First logged mean follows three stride-3 inference calls.
            for jitter in (-6, 0, 6):
                end = min(len(features), max(1, 3*(index+3)+jitter))
                window = features[max(0, end-220):end]
                if len(window) < 220:
                    window = np.pad(window, ((220-len(window), 0), (0, 0)))
                windows.append(window)
        return windows, dict(file=str(path.relative_to(root)), peak=float(scores.max()),
                             windows=len(windows))

    windows, records = [], []
    with ThreadPoolExecutor(max_workers=3) as pool:
        for i, (features, record) in enumerate(pool.map(mine, sorted(paths)), 1):
            windows.extend(features)
            if record:
                records.append(record)
            if i % 500 == 0:
                print('processed', i, '/', len(paths), 'windows', len(windows), flush=True)
    if not windows:
        raise RuntimeError('no challenging negatives found')
    os.umask(0o077)
    with args.output.open('xb') as output:
        np.savez_compressed(output, x=np.stack(windows))
    args.output.with_suffix('.json').write_text(json.dumps({
        'source': 'public training splits only; no test/validation clips',
        'clips_scanned': len(paths), 'selected_clips': records, 'windows': len(windows),
    }, indent=2)+'\n')
    print('complete', len(windows), 'windows', flush=True)


if __name__ == '__main__':
    main()
