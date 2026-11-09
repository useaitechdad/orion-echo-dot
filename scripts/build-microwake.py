#!/usr/bin/env python3
"""Build the isolated microWakeWord helper from explicit, pinned dependencies.

No download, deployment, microphone access or model training. See native/microwake/README.md.
"""
import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--frontend', required=True, type=Path, help='unpacked pymicro_features-2.0.2 source')
    p.add_argument('--library', required=True, type=Path, help='matching TensorFlow Lite C library')
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--compiler', default='clang++', help='compiler command, e.g. zig c++ -target arm-linux-gnueabihf.2.34')
    args = p.parse_args()
    root = Path(__file__).resolve().parent.parent
    src = args.frontend.resolve()
    front = src / 'tensorflow/lite/experimental/microfrontend/lib'
    names = ['kiss_fft_int16', 'fft', 'fft_util', 'filterbank', 'filterbank_util',
             'frontend', 'frontend_util', 'log_lut', 'log_scale', 'log_scale_util',
             'noise_reduction', 'noise_reduction_util', 'pcan_gain_control',
             'pcan_gain_control_util', 'window', 'window_util']
    files = [front / (name + '.cc') for name in names]
    files += [src / 'kissfft/kiss_fft.cc', src / 'kissfft/tools/kiss_fftr.cc',
              root / 'native/microwake/main.cc']
    if args.output.exists():
        p.error('output already exists; use a new build path')
    args.output.parent.mkdir(parents=True, exist_ok=True)
    cmd = shlex.split(args.compiler) + ['-O2', '-std=c++17', '-DFIXED_POINT=16',
           '-I' + str(src), '-I' + str(src / 'kissfft')]
    cmd += list(map(str, files)) + [str(args.library.resolve()), '-lm', '-o', str(args.output)]
    if args.library.suffix == '.dylib':
        cmd += ['-Wl,-rpath,' + str(args.library.resolve().parent)]
    else:
        # Required when the ARM executable is invoked through its isolated
        # glibc loader on FireOS. Non-PIE initialized libraries then crashed
        # before main on the tested device.
        cmd += ['-fPIE', '-pie']
    print(json.dumps({'output': str(args.output), 'frontend': str(src)}), flush=True)
    subprocess.run(cmd, check=True, env=os.environ.copy())


if __name__ == '__main__':
    main()
