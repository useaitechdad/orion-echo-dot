#!/usr/bin/env python3
"""Adapt an existing candidate locally with explicitly supplied consented audio.

No network, microphone or deployment. Output weights and features are private.
Keep a different recording out of training and validate the exported runtime.
Requires the pinned upstream training source on PYTHONPATH.
"""
import argparse
import csv
import json
import math
import os
from pathlib import Path
import random

import numpy as np
from pymicro_features import MicroFrontend
from scipy.signal import fftconvolve, resample, resample_poly
import soundfile as sf
import yaml


def frontend(audio):
    raw = np.round(np.clip(audio, -1, 1) * 32767).astype('<i2').tobytes()
    engine = MicroFrontend()
    rows = []
    for i in range(0, len(raw) - 319, 320):
        result = engine.process_samples(raw[i:i+320])
        if result.features:
            rows.append(result.features)
    return np.asarray(rows, dtype=np.float32)[-220:]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--clip', type=Path, required=True, help='16 kHz mono signed-16 raw PCM')
    p.add_argument('--segments', required=True, help='JSON array of [start,end] seconds')
    p.add_argument('--public-npz', type=Path, required=True)
    p.add_argument('--checkpoint', type=Path, required=True)
    p.add_argument('--esc50', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--steps', type=int, default=1000)
    p.add_argument('--initial-weights', type=Path)
    p.add_argument('--hard-negatives', type=Path)
    p.add_argument('--learning-rate', type=float, default=.00005)
    args = p.parse_args()
    if args.output.exists():
        p.error('output exists; choose a new candidate directory')
    os.umask(0o077)
    args.output.mkdir(parents=True, mode=0o700)
    rng = random.Random(6111)
    nrng = np.random.default_rng(6111)
    clip = np.frombuffer(args.clip.read_bytes(), '<i2').astype(np.float32) / 32768
    segments = json.loads(args.segments)
    if len(segments) < 2 or any(not 0 <= a < b <= len(clip)/16000 or b-a > 3 for a, b in segments):
        p.error('invalid training phrase boundaries')
    phrases = [clip[round(a*16000):round(b*16000)] for a, b in segments]
    with (args.esc50 / 'meta/esc50.csv').open() as f:
        noise_paths = [args.esc50 / 'audio' / row['filename']
                       for row in csv.DictReader(f) if int(row['fold']) <= 3]
    if not noise_paths:
        p.error('no training-fold background sounds')
    noise_cache = {}

    def augmented(phrase):
        phrase = resample(phrase, round(len(phrase) * rng.uniform(.88, 1.12)))
        audio = np.zeros(64000, dtype=np.float32)
        end = len(audio) - round(rng.uniform(.05, .25)*16000)
        audio[end-len(phrase):end] = phrase
        if rng.random() < .5:
            rir = np.zeros(4000, dtype=np.float32)
            rir[0] = 1
            for _ in range(8):
                k = rng.randrange(160, len(rir))
                rir[k] += rng.uniform(-.2, .2) * math.exp(-k/2000)
            audio = fftconvolve(audio, rir)[:len(audio)]
        if rng.random() < .8:
            path = rng.choice(noise_paths)
            if path not in noise_cache:
                noise, sr = sf.read(path, dtype='float32')
                d = math.gcd(sr, 16000)
                noise_cache[path] = resample_poly(noise, 16000//d, sr//d)
            noise = noise_cache[path]
            start = rng.randrange(max(1, len(noise)-len(audio)))
            noise = np.resize(noise[start:], len(audio))
            signal_rms = max(float(np.sqrt(np.mean(phrase**2))), 1e-6)
            noise_rms = max(float(np.sqrt(np.mean(noise**2))), 1e-6)
            audio += noise * signal_rms/noise_rms * 10**(-rng.uniform(5, 30)/20)
        audio *= 10**(rng.uniform(-6, 8)/20)
        return frontend(audio)

    private = np.stack([augmented(phrases[i % len(phrases)]) for i in range(900)])
    np.save(args.output / 'private-training-features.npy', private)
    print('prepared local training examples', len(private), flush=True)
    data = np.load(args.public_npz, allow_pickle=False)
    if (data['x'].dtype != np.uint16 or data['x'].ndim != 3 or
            data['x'].shape[1:] != (220, 40) or
            data['y'].shape != (len(data['x']),) or
            not np.isin(data['y'], [0, 1]).all()):
        raise ValueError('public NPZ requires uint16 x[N,220,40] and binary y[N]')
    public = data['x'].astype(np.float32) * .0390625
    labels = data['y']
    pos = np.flatnonzero(labels > .5)
    neg = np.flatnonzero(labels < .5)
    if len(pos) < 100 or len(neg) < 1000:
        raise ValueError('insufficient public preservation data')

    import tensorflow as tf
    from microwakeword import mixednet, utils
    from microwakeword.layers import modes
    tf.config.threading.set_intra_op_parallelism_threads(4)
    tf.config.threading.set_inter_op_parallelism_threads(2)
    tf.keras.utils.set_random_seed(6111)
    config = yaml.full_load((args.checkpoint / 'training_config.yaml').read_text())
    flags = argparse.Namespace(**config['flags'])
    model = mixednet.model(flags, shape=config['training_input_shape'], batch_size=64)
    model.load_weights(str(args.initial_weights or args.checkpoint / 'best_weights.weights.h5'))
    model.compile(optimizer=tf.keras.optimizers.Adam(learning_rate=args.learning_rate),
                  loss=tf.keras.losses.BinaryCrossentropy())
    y = np.concatenate([np.ones(32), np.zeros(32)]).astype(np.float32).reshape(-1, 1)
    weights = np.concatenate([np.full(16, 1.5), np.ones(16), np.full(32, 2.)]).astype(np.float32)
    hard = np.load(args.hard_negatives)['x'] if args.hard_negatives else None
    if hard is not None:
        if hard.shape[1:] != (220, 40):
            raise ValueError('incorrect hard-negative feature shape')
        weights[48:] = 3
    for step in range(1, args.steps+1):
        x = np.concatenate([private[nrng.integers(len(private), size=16)],
                            public[nrng.choice(pos, size=16)], public[nrng.choice(neg, size=32)]])
        if hard is not None:
            x[48:] = hard[nrng.integers(len(hard), size=16)]
        loss = model.train_on_batch(x, y, sample_weight=weights)
        if step % 100 == 0:
            print('step', step, 'loss', float(loss), flush=True)
    model.save_weights(str(args.output / 'adapted.weights.h5'))
    # Rebuild batch=1 before streaming conversion. Public/private calibration
    # remains local and consists only of training examples.
    inference = mixednet.model(flags, shape=config['training_input_shape'], batch_size=1)
    inference.load_weights(str(args.output / 'adapted.weights.h5'))
    config['train_dir'] = str(args.output.resolve())
    utils.convert_model_saved(inference, config, folder='stream_state_internal',
                              mode=modes.Modes.STREAM_INTERNAL_STATE_INFERENCE)
    representative = np.concatenate([private[:100], public[nrng.choice(pos, size=100)],
                                     public[nrng.choice(neg, size=300)]])

    class Calibration:
        def get_data(self, *unused, **kwargs):
            return representative[:, :kwargs['features_length']], None, None

    config['spectrogram_length'] = 219
    utils.convert_saved_model_to_tflite(config, audio_processor=Calibration(),
        path_to_model=str(args.output / 'stream_state_internal'),
        folder=str(args.output), fname='model.tflite', quantize=True)
    (args.output / 'provenance.json').write_text(json.dumps({
        'status': 'private personalized candidate; validation required',
        'private_training_recordings': 1, 'private_training_phrases': len(phrases),
        'private_augmented_examples': len(private), 'public_examples': len(public),
        'steps': args.steps, 'seed': 6111, 'network_used': False,
        'hard_negative_windows': 0 if hard is None else len(hard),
        'learning_rate': args.learning_rate,
        'publication': 'excluded from public source package',
    }, indent=2) + '\n')
    print('exported private candidate', flush=True)


if __name__ == '__main__':
    main()
