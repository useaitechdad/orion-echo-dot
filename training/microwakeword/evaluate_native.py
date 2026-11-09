#!/usr/bin/env python3
"""Evaluate an isolated native candidate on public/synthetic held-out audio.

Clips reset frontend/model state. Counts are corpus observations, not a measured
continuous household false-wake rate. No microphone or network is used.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import csv
import json
import math
import os
from pathlib import Path
import struct
import subprocess
import time

import numpy as np
from scipy.signal import resample_poly
import soundfile as sf


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--workspace', type=Path, required=True)
    p.add_argument('--helper', type=Path, required=True)
    p.add_argument('--model', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--workers', type=int, default=3)
    p.add_argument('--limit', type=int, default=0)
    p.add_argument('--synthetic', default='synthetic')
    p.add_argument('--thresholds', type=float, nargs='+', default=[.5, .75, .9, .95])
    args = p.parse_args()
    if args.output.exists():
        p.error('output exists; preserve it and choose a new name')
    root = args.workspace.resolve()
    jobs = []
    for group, pattern in [('target', args.synthetic + '/testing/positive/*.wav'),
                           ('confusable', args.synthetic + '/testing/negative/*.wav'),
                           ('speech', 'speech_raw/LibriSpeech/test-clean/*/*/*.flac')]:
        paths = sorted(root.glob(pattern))
        if not paths:
            p.error(f'empty evaluation group {group}: check workspace and --synthetic')
        jobs.extend((group, x) for x in (paths[:args.limit] if args.limit else paths))
    esc = root / 'environmental/ESC-50-master'
    with (esc / 'meta/esc50.csv').open() as f:
        rows = [r for r in csv.DictReader(f) if int(r['fold']) == 5]
    if not rows:
        p.error('empty evaluation group environment: ESC-50 fold 5 required')
    jobs.extend(('environment', esc / 'audio' / r['filename'])
                for r in (rows[:args.limit] if args.limit else rows))
    thresholds = args.thresholds
    if any(not .5 <= t < 1 for t in thresholds):
        p.error('thresholds must be at least .5 and less than 1')

    def run(job):
        group, path = job
        audio, sr = sf.read(path, dtype='float32')
        if audio.ndim != 1:
            audio = audio.mean(axis=1)
        duration = len(audio) / sr
        divisor = math.gcd(sr, 16000)
        audio = resample_poly(audio, 16000 // divisor, sr // divisor)
        raw = np.round(np.clip(audio, -1, 1) * 32767).astype('<i2').tobytes() + bytes(32000)
        framed = b''.join(struct.pack('<H', len(raw[i:i+1024])) + raw[i:i+1024]
                          for i in range(0, len(raw), 1024))
        started = time.monotonic()
        result = subprocess.run([str(args.helper.resolve()), str(args.model.resolve()), '.5'],
            input=framed, capture_output=True, check=True,
            env={**os.environ, 'ORION_WAKE_SCORES': '1'})
        elapsed = time.monotonic() - started
        scores = [float(s.split('=')[1]) for s in result.stderr.decode().splitlines()
                  if s.startswith('wake_probability=')]
        if not scores or not result.stdout.startswith(b'R'):
            raise RuntimeError('missing native scores or protocol readiness')
        hits = {}
        for threshold in thresholds:
            armed, count = True, 0
            for score in scores:
                if armed and score > threshold:
                    count += 1
                    armed = False
                if score < threshold * .5:
                    armed = True
            hits[str(threshold)] = count
        return dict(group=group, file=str(path.relative_to(root)), seconds=duration,
                    elapsed_seconds=elapsed, peak=max(scores), hits=hits)

    results = []
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        for result in pool.map(run, jobs):
            results.append(result)
            if len(results) % 200 == 0:
                print('completed', len(results), '/', len(jobs), flush=True)
    summary = {}
    for group in sorted(set(r['group'] for r in results)):
        records = [r for r in results if r['group'] == group]
        summary[group] = dict(clips=len(records), seconds=sum(r['seconds'] for r in records),
            thresholds={str(t): dict(events=sum(r['hits'][str(t)] for r in records),
                clips_with_events=sum(r['hits'][str(t)] > 0 for r in records)) for t in thresholds})
    report = dict(model=args.model.name, reset_between_clips=True, tail_silence_seconds=1,
                  note='Public corpus observations; not a household false-activation estimate',
                  summary=summary, clips=results)
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(summary, indent=2), flush=True)


if __name__ == '__main__':
    main()
