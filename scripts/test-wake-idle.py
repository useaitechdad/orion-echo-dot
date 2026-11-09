#!/usr/bin/env python3
"""Native wake regression; inputs are 16 kHz mono S16 WAVs or raw .s16le.

Usage: python3 scripts/test-wake-idle.py HELPER MODEL POSITIVE [NEGATIVE ...]
POSITIVE must contain exactly three separated "Hey Orion" phrases.
Uses the same framed pipe protocol as the Go client; no device or network.
Private, consented calibration clips may be passed by external path; this
script prints counts only and never copies audio into the project.
"""
import random
from pathlib import Path
import struct
import subprocess
import sys
import wave


def read_pcm(path):
    if Path(path).suffix == ".s16le":
        pcm = Path(path).read_bytes()
        assert pcm and len(pcm) % 2 == 0, "empty or malformed raw fixture"
        return pcm
    with wave.open(path, "rb") as wav:
        assert (wav.getnchannels(), wav.getsampwidth(), wav.getframerate()) == (1, 2, 16000)
        assert wav.getnframes() > 0, "empty WAV fixture"
        return wav.readframes(wav.getnframes())


class Helper:
    def __init__(self, helper, model):
        self.process = subprocess.Popen(
            [helper, model, "1e-34"], stdin=subprocess.PIPE, stdout=subprocess.PIPE
        )
        assert self.process.stdout.read(1) == b"R"

    def packet(self, pcm):
        self.process.stdin.write(struct.pack("<H", len(pcm)) + pcm)
        self.process.stdin.flush()
        reply = self.process.stdout.read(1)
        assert reply in (b"0", b"1"), "helper failed or invalid protocol reply"
        return reply == b"1"

    def feed(self, pcm, chunk=1024):
        return sum(self.packet(pcm[i:i + chunk]) for i in range(0, len(pcm), chunk))

    def close(self):
        self.process.stdin.close()
        assert self.process.wait(timeout=5) == 0


def main():
    helper, model, positive, *negatives = sys.argv[1:]
    speech = read_pcm(positive) + bytes(32000)
    cases = [("no lead", b"", 3), ("30s silence", bytes(30 * 32000), 3)]
    for rms in (10, 30, 100):
        rng = random.Random(4)
        noise = struct.pack("<16000h", *(round(rng.gauss(0, rms)) for _ in range(16000)))
        cases.append((f"30s noise RMS{rms}", noise * 30, 3))
    for label, lead, minimum in cases:
        detector = Helper(helper, model)
        try:
            counts = []
            for _ in range(3):
                assert not detector.packet(b"")
                assert detector.feed(lead) == 0, f"false activation: {label}"
                hits = detector.feed(speech)
                counts.append(hits)
                assert minimum <= hits <= 3, (label, counts)
            print(label, counts, flush=True)
        finally:
            detector.close()
    # Exercise packets smaller/larger than the 480-sample VAD frame,
    # including reset with an incomplete frame still buffered.
    for chunk in (128, 4096):
        detector = Helper(helper, model)
        try:
            assert not detector.packet(bytes(126))
            assert not detector.packet(b"")
            assert detector.feed(speech, chunk) == 3
        finally:
            detector.close()
    detector = Helper(helper, model)
    try:
        for path in negatives:
            assert not detector.packet(b"")
            assert detector.feed(read_pcm(path) + bytes(32000)) == 0, path
            print("negative passed:", path, flush=True)
    finally:
        detector.close()
    print("PASS: idle/reset/framing regressions; real-voice accuracy remains a separate check")


if __name__ == "__main__":
    main()
