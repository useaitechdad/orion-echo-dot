#!/usr/bin/env python3
"""File-only protocol/recognition regression for the microWakeWord helper."""
import argparse
import json
from pathlib import Path
import struct
import subprocess
import wave


def pcm(path):
    path = Path(path)
    if path.suffix == '.s16le':
        data = path.read_bytes()
    else:
        with wave.open(str(path)) as w:
            assert (w.getnchannels(), w.getsampwidth(), w.getframerate()) == (1, 2, 16000)
            data = w.readframes(w.getnframes())
    assert data and len(data) % 2 == 0
    return data


def framed(data, chunk=1024):
    return b''.join(struct.pack('<H', len(data[i:i+chunk])) + data[i:i+chunk]
                    for i in range(0, len(data), chunk))


def run(command, packets):
    r = subprocess.run(command, input=packets, capture_output=True, timeout=120)
    if r.returncode:
        raise RuntimeError(f'helper exited {r.returncode}: {r.stderr[-1000:]!r}')
    assert r.stdout[:1] == b'R', 'missing ready handshake'
    assert all(c in b'01' for c in r.stdout[1:]), 'invalid protocol output'
    count = 0
    offset = 0
    while offset < len(packets):
        size, = struct.unpack_from('<H', packets, offset)
        offset += 2 + size
        count += 1
    assert offset == len(packets) and len(r.stdout) == count + 1, 'packet/reply mismatch'
    return r.stdout[1:]


def main():
    p = argparse.ArgumentParser()
    p.add_argument('helper')
    p.add_argument('model')
    p.add_argument('--threshold', type=float, default=.9)
    p.add_argument('--positive', action='append', default=[], help='path=expected_count')
    p.add_argument('--negative', action='append', default=[])
    args = p.parse_args()
    command = [args.helper, args.model, str(args.threshold)]
    failures = []
    reset = b'\0\0'
    # Reset an incomplete 10 ms frontend frame and all model state.
    prefix = framed(bytes(126)) + reset
    assert b'1' not in run(command, prefix + framed(bytes(30 * 32000)))
    for value in args.positive:
        path, expected = value.rsplit('=', 1)
        audio = pcm(path) + bytes(32000)
        counts = []
        for chunk in (128, 1024, 4096):
            replies = run(command, prefix + framed(audio, chunk))
            counts.append(replies.count(b'1'))
        repeated = run(command, (reset + framed(audio)) * 3).count(b'1')
        row = dict(fixture=Path(path).name, counts_by_chunk=counts, repeated=repeated,
                   expected=int(expected), threshold=args.threshold)
        print(json.dumps(row), flush=True)
        if counts != [int(expected)] * 3 or repeated != 3 * int(expected):
            failures.append(row)
    for path in args.negative:
        count = run(command, reset + framed(pcm(path) + bytes(32000))).count(b'1')
        print(json.dumps(dict(fixture=Path(path).name, negative_detections=count)), flush=True)
        if count:
            failures.append(path)
    for bad in (b'\x01', b'\x01\x00x', b'\x02\x00x', b'\x02\x10'):
        r = subprocess.run(command, input=bad, capture_output=True, timeout=10)
        assert r.returncode != 0, 'accepted malformed/truncated input'
    r = subprocess.run([args.helper, args.model, '1e-34'], capture_output=True, timeout=10)
    assert b'threshold must be finite' in r.stderr, 'missing threshold diagnostic'
    assert r.returncode != 0, 'accepted PocketSphinx threshold'
    if failures:
        raise SystemExit('FAIL: recognition counts do not meet the supplied expectations')
    print('PASS: framing, reset, invalid input, silence and supplied recognition fixtures')


if __name__ == '__main__':
    main()
