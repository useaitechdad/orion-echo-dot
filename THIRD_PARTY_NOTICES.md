# Third-party notices

Original Orion code is covered by [MIT](LICENSE). Third-party code, models,
data and tools retain their own licenses; MIT does not replace those terms.

- `native/microwake/tflite_abi.h` adapts declarations from TensorFlow Lite C
  headers, copyright The TensorFlow Authors, Apache License 2.0. See
  [license text](LICENSES/Apache-2.0.txt) and
  [upstream headers](https://github.com/tensorflow/tensorflow/tree/v2.17.1/tensorflow/lite/core/c).
- PocketSphinx code/assets retain [their license](assets/wake/LICENSE.pocketsphinx)
  and [acoustic-model notice](assets/wake/en-us/README).
- The microfrontend build compiles Kiss FFT, copyright Mark Borgerding, under
  [BSD-3-Clause](LICENSES/kissfft-BSD-3-Clause.txt). Retain this notice in binary bundles.
- Go modules retain licenses in their upstream distributions.
- Downloaded TensorFlow Lite, microfrontend, microWakeWord, Piper, glibc and
  libstdc++ are separate dependencies. Retain upstream notices and comply with
  their source/redistribution requirements before distributing a binary bundle.
- Dataset and voice/model terms are separate from source-code licenses. See
  [training attribution](training/microwakeword/README.md). No recordings,
  generated speech fixtures or personal wake weights are supplied here.
