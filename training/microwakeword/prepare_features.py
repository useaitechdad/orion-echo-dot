#!/usr/bin/env python3
"""Build 10 ms microfrontend features from synthetic/public WAV/FLAC inputs.

Private calibration recordings are deliberately not inputs to this program.
Speaker splits and ESC-50 folds are retained; no validation/test noise is mixed
into training positives. Existing completed features are never overwritten.
"""
import argparse
from concurrent.futures import ProcessPoolExecutor
import csv
import hashlib
import json
import math
import os
from pathlib import Path
import random

import numpy as np
from mmap_ninja.ragged import RaggedMmap
from pymicro_features import MicroFrontend
from scipy.signal import butter, fftconvolve, resample, resample_poly, sosfilt
import soundfile as sf

ROOT = None
ENVIRONMENT = {}
BACKGROUND = {}
REVISION = 'v1'


def read_audio(path):
    audio, rate = sf.read(path, dtype='float32')
    if audio.ndim > 1:
        audio = audio.mean(axis=1)
    if rate != 16000:
        divisor = math.gcd(rate, 16000)
        audio = resample_poly(audio, 16000 // divisor, rate // divisor)
    return audio.astype(np.float32)


def initialize(root, revision='v1'):
    global ROOT, ENVIRONMENT, BACKGROUND, REVISION
    ROOT = Path(root)
    REVISION = revision
    ENVIRONMENT = {s: [] for s in ['training', 'validation', 'testing']}
    base = ROOT / 'environmental/ESC-50-master'
    with (base / 'meta/esc50.csv').open() as f:
        for row in csv.DictReader(f):
            fold = int(row['fold'])
            split = 'training' if fold <= 3 else 'validation' if fold == 4 else 'testing'
            ENVIRONMENT[split].append(base / 'audio' / row['filename'])
    BACKGROUND = {}


def augment(audio, split, rng):
    if REVISION == 'v2':
        # Vary pitch/speed, microphone response, and unaligned array averaging.
        # These are synthetic transforms, not measurements of a private room.
        audio = resample(audio, round(len(audio) * rng.uniform(.85, 1.15)))
        if rng.random() < .65:
            cutoff = rng.uniform(2200, 7400)
            audio = sosfilt(butter(3, cutoff, fs=16000, output='sos'), audio)
        if rng.random() < .6:
            kernel = np.zeros(7, dtype=np.float32)
            for _ in range(7):
                kernel[rng.randrange(7)] += 1 / 7
            audio = fftconvolve(audio, kernel)[:len(audio)]
    # Keep complete target phrases; overlong synthesized examples are rejected.
    if len(audio) > 3 * 16000:
        raise ValueError('Synthetic phrase exceeds 3 seconds; inspect generation')
    total = 4 * 16000
    trailing = int(rng.uniform(.08, .22) * 16000)
    speech = np.zeros(total, dtype=np.float32)
    stop = total - trailing
    speech[stop-len(audio):stop] = audio
    if rng.random() < (.75 if REVISION == 'v2' else .5):
        # Deterministic, synthetic room response; no private room recording.
        duration = rng.uniform(.2, .7) if REVISION == 'v2' else rng.uniform(.12, .4)
        rir = np.zeros(int(duration * 16000), dtype=np.float32)
        rir[0] = 1
        for _ in range(45 if REVISION == 'v2' else 15):
            k = rng.randrange(160, len(rir))
            decay = .25 if REVISION == 'v2' else .12
            amplitude = .5 if REVISION == 'v2' else .3
            rir[k] += rng.uniform(-amplitude, amplitude) * math.exp(-k / (16000 * decay))
        speech = fftconvolve(speech, rir)[:total].astype(np.float32)
    if rng.random() < .8:
        path = rng.choice(ENVIRONMENT[split])
        if path not in BACKGROUND:
            BACKGROUND[path] = read_audio(path)
        noise = BACKGROUND[path]
        offset = rng.randrange(max(1, len(noise) - total + 1))
        noise = np.resize(noise[offset:], total).copy()
        signal_rms = max(float(np.sqrt(np.mean(audio ** 2))), 1e-6)
        noise_rms = max(float(np.sqrt(np.mean(noise ** 2))), 1e-6)
        snr = rng.uniform(-3, 25) if REVISION == 'v2' else rng.uniform(3, 25)
        noise *= signal_rms / noise_rms * 10 ** (-snr / 20)
        speech += noise
    speech *= 10 ** (rng.uniform(-35, 0) / 20)
    return np.clip(speech, -1, 1)


def features(audio):
    pcm = np.round(np.clip(audio, -1, 1) * 32767).astype('<i2').tobytes()
    frontend = MicroFrontend()
    rows = []
    for offset in range(0, len(pcm) - 319, 320):
        result = frontend.process_samples(pcm[offset:offset+320])
        if result.samples_read != 160:
            raise RuntimeError('frontend packet consumption mismatch')
        if result.features:
            rows.append(result.features)
    # Store the exact frontend integer representation. Upstream loader converts
    # uint16 features to float with 0.0390625, matching native inference.
    return np.round(np.asarray(rows, dtype=np.float32) * 25.6).astype(np.uint16)


def process(job):
    path, split, kind, repeat = job
    audio = read_audio(path)
    if kind in ('positive', 'hard_negative'):
        seed = int(hashlib.sha256((str(Path(path).relative_to(ROOT)) + ':' + str(repeat)).encode()).hexdigest()[:16], 16)
        audio = augment(audio, split, random.Random(seed))
    elif len(audio) < 3 * 16000:
        audio = np.pad(audio, (0, 3 * 16000 - len(audio)))
    matrix = features(audio)
    if kind in ('positive', 'hard_negative'):
        return [matrix]
    # Independent negative examples remain from one source utterance/fold.
    chunks = [matrix[i:i+500] for i in range(20, len(matrix), 500)]
    return [c for c in chunks if len(c) >= 250]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--workspace', type=Path, required=True)
    p.add_argument('--workers', type=int, default=3)
    p.add_argument('--revision', choices=['v1', 'v2'], default='v1')
    p.add_argument('--groups', nargs='+', default=['positive', 'hard_negative', 'environment', 'speech'])
    args = p.parse_args()
    root = args.workspace.resolve()
    initialize(root, args.revision)
    feature_dir = root / ('features-v2' if args.revision == 'v2' else 'features')
    synthetic_dir = root / ('synthetic-v2' if args.revision == 'v2' else 'synthetic')
    manifest = {}
    for kind in args.groups:
        for split in ['training', 'validation', 'testing']:
            output = feature_dir / kind / split / 'clips_mmap'
            complete = output.parent / 'complete.json'
            if complete.exists():
                continue
            if output.exists():
                raise RuntimeError(f'Partial output preserved at {output}; use a new revision')
            if kind in ('positive', 'hard_negative'):
                label = 'positive' if kind == 'positive' else 'negative'
                directory = synthetic_dir / split / label
                if not (directory / 'complete.json').exists():
                    raise RuntimeError(f'Synthesis incomplete: {directory}')
                paths = sorted(directory.glob('*.wav'))
            elif kind == 'environment':
                paths = ENVIRONMENT[split]
            elif kind == 'speech':
                corpus = 'test-clean' if split == 'testing' else 'dev-clean'
                paths = []
                for path in sorted((root / 'speech_raw/LibriSpeech' / corpus).glob('*/*/*.flac')):
                    speaker = int(path.parent.parent.name)
                    if split == 'testing' or ((speaker % 5 == 0) == (split == 'validation')):
                        paths.append(path)
            else:
                raise ValueError(kind)
            if not paths:
                raise RuntimeError(f'No source files for {kind}/{split}')
            repeats = 2 if split == 'training' and kind in ('positive', 'hard_negative') else 1
            jobs = [(str(path), split, kind, rep) for path in paths for rep in range(repeats)]
            output.parent.mkdir(parents=True, exist_ok=True)
            stats = {'sources': len(paths), 'examples': 0, 'frames': 0, 'frame_step_ms': 10,
                     'revision': args.revision}
            def stream(pool):
                for matrices in pool.map(process, jobs, chunksize=8):
                    for matrix in matrices:
                        stats['examples'] += 1
                        stats['frames'] += len(matrix)
                        if stats['examples'] % 500 == 0:
                            print(kind, split, stats['examples'], flush=True)
                        yield matrix
            with ProcessPoolExecutor(max_workers=args.workers, initializer=initialize,
                                     initargs=(root, args.revision)) as pool:
                RaggedMmap.from_generator(out_dir=str(output), sample_generator=stream(pool), batch_size=128, verbose=False)
            complete.write_text(json.dumps(stats, indent=2) + '\n')
            manifest[f'{kind}/{split}'] = stats
            print('COMPLETE', kind, split, stats, flush=True)
            if kind in ('environment', 'speech') and split in ('validation', 'testing'):
                ambient = output.parent.parent / (split + '_ambient')
                if not ambient.exists():
                    ambient.symlink_to(split, target_is_directory=True)
    (feature_dir / 'latest-preparation.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    main()
