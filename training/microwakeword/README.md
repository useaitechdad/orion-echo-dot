# Wake-model training tools

These scripts train a small phrase detector and export a streaming quantized
TensorFlow Lite model for the native helper. They are research/build tools, not
a complete downloaded training environment. Obtain the pinned upstream sources,
dependencies and public datasets listed in `dependencies.json`. No personal
recordings, feature arrays, trained weights or checkpoints are included.

## Pipeline

1. `synthesize.py --workspace PATH` creates target/confusable phrases using Piper
   and disjoint speaker-ID pools for training, validation and testing.
2. `prepare_features.py --workspace PATH` creates 10 ms microfrontend features
   with noise, room-response and gain augmentation.
3. `train.py --workspace PATH --name MODEL_NAME` trains a MixedNet candidate from
   prepared feature splits. This script requires a GPU and rejects existing
   candidate/configuration destinations.
4. `export.py CHECKPOINT NEW_OUTPUT` exports the streaming quantized model.
5. `evaluate_native.py --workspace PATH --helper HELPER --model MODEL_DIR
   --synthetic synthetic-v2 --output NEW_REPORT` evaluates the actual native helper on held-out public audio.
6. Check the exported model on target hardware, including reset, idle, privacy,
   latency and realistic negative inputs before deployment.

Use each script's `--help` for its inputs. The upstream training code must be
available on `PYTHONPATH`; the scripts expect a prepared workspace. Version pins
record the demonstrated environment, not a promise of compatibility with all
later dependency versions. Synthesis and TensorFlow training may require separate
environments.

The optional `--revision v2` synthesis/feature path broadens phrase variants and
acoustic augmentation. `train.py --features features-v2 --name MODEL_NAME`
selects that feature set. Keep candidate outputs distinct so comparisons remain
reproducible. The streaming model uses three-frame chunks; quantization
calibration must use a compatible window length.

## Data splits

- Piper speaker IDs: 0–599 training, 600–699 validation, 700–799 testing.
- ESC-50: folds 1–3 training, fold 4 validation, fold 5 testing. Mix background
  audio only from the matching split.
- LibriSpeech: dev-clean speakers split into training/validation; test-clean
  remains held out.
- The runtime uses a 10 ms frontend. Legacy 20 ms feature archives are incompatible.

Evaluate recall and false activations separately. Reset-between-clip evaluation
is not a continuous household soak. If results guide thresholds or revisions,
those observations are development evidence rather than an independent accuracy
estimate. Keep fresh evaluation material separate.

## Consented local adaptation

`adapt_local.py` accepts an explicit raw-PCM clip, phrase boundaries, a pretrained
checkpoint, public training features and ESC-50 training audio. It contains no
network or microphone capture code. It writes new private outputs without
replacing an existing candidate. Obtain consent, use local paths outside this
source folder, and keep another recording out of training.

`mine_local_negatives.py` selects difficult windows from the public **training**
split. Do not mine held-out tests for training. Personal recordings, features,
adapted weights and exported personal models must remain private; do not upload
them merely because the surrounding tools are public.

## Attribution and redistribution

Dependencies include OHF-Voice/micro-wake-word, OHF-Voice/pymicro-wakeword,
rhasspy/pymicro-features and rhasspy/piper-sample-generator. Public data sources
include ESC-50 and OpenSLR LibriSpeech. Preserve their notices and review the
precise dataset and voice-checkpoint terms before distributing trained weights.
The [ESC-50 license](https://github.com/karolpiczak/ESC-50#license) includes
noncommercial restrictions; [LibriSpeech](https://www.openslr.org/12) uses CC BY
4.0. This source package does not grant redistribution rights for resulting
models or claim they are cleared for commercial use.

## Workspace and environments

Use a private workspace outside the repository. Commands below are run from the
repository root. Set `ORION_WORKSPACE` to your chosen **absolute** path:

```text
WORKSPACE/
  training-source/                   # pinned OHF micro-wake-word checkout
  piper-source/                      # pinned piper-sample-generator checkout
  models/en_US-libritts_r-medium.pt
  environmental/ESC-50-master/{audio/,meta/esc50.csv}
  speech_raw/LibriSpeech/{dev-clean/,test-clean/}
  synthetic-v2/{training,validation,testing}/{positive,negative}/
  features-v2/{positive,hard_negative,environment,speech}/
  trained_models/candidate-v2/
  private/                           # your calibration, held-out audio and adaptations
```

The synthetic/features/trained_models folders are produced by the scripts. Obtain
inputs from the URLs in `dependencies.json`. Extract ESC-50 under `environmental`
and the two LibriSpeech archives under `speech_raw`. Match **each downloaded
archive/checkpoint SHA-256** to that file before extraction/use. If a mutable
upstream archive differs, stop and review the new revision; do not bypass its
check. Dataset acquisition and model preparation are separate from device setup.

```sh
export ORION_WORKSPACE=/path/to/private/orion-training
mkdir -p "$ORION_WORKSPACE"
git clone https://github.com/OHF-Voice/micro-wake-word "$ORION_WORKSPACE/training-source"
git -C "$ORION_WORKSPACE/training-source" checkout 4665173cd35f1cff9a61e06fc427f124766c488e
git clone https://github.com/rhasspy/piper-sample-generator "$ORION_WORKSPACE/piper-source"
git -C "$ORION_WORKSPACE/piper-source" checkout 2971426a55072f7d22fec416ca7800df8bd23207
uv venv --python 3.11 "$ORION_WORKSPACE/train-env"
uv pip install --python "$ORION_WORKSPACE/train-env/bin/python" \
  -r training/microwakeword/requirements-training.txt -e "$ORION_WORKSPACE/training-source"
uv venv --python 3.11 "$ORION_WORKSPACE/synth-env"
uv pip install --python "$ORION_WORKSPACE/synth-env/bin/python" \
  torch==2.5.1+cu124 torchaudio==2.5.1+cu124 --index-url https://download.pytorch.org/whl/cu124
uv pip install --python "$ORION_WORKSPACE/synth-env/bin/python" \
  -e "$ORION_WORKSPACE/piper-source" 'torch==2.5.1+cu124' 'torchaudio==2.5.1+cu124'
```

These CUDA instructions target Linux with an NVIDIA GPU; install a compatible
driver first. They do not configure CUDA on a Mac. The upstream editable install
supplies its declared dependencies (including audiomentations, audio_metadata,
datasets, webrtcvad-wheels and ai-edge-litert for training). The requirements file
adds/pins this package's direct imports. It is not a complete transitive lockfile;
record `pip freeze` privately after resolving an environment. Install system
libs required by your chosen soundfile/eSpeak/Piper wheels if the platform reports
missing libraries. Keep TensorFlow and Torch environments separate.

```sh
"$ORION_WORKSPACE/synth-env/bin/python" training/microwakeword/synthesize.py \
  --workspace "$ORION_WORKSPACE" --revision v2
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/prepare_features.py \
  --workspace "$ORION_WORKSPACE" --revision v2
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/train.py \
  --workspace "$ORION_WORKSPACE" --features features-v2 --name candidate-v2
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/export.py \
  "$ORION_WORKSPACE/trained_models/candidate-v2" "$ORION_WORKSPACE/export-v2"
```

Synthesis supports `--smoke` for a small dependency check. Its `*-smoke` outputs
are intentionally not consumed by the full training pipeline. Training requires
all splits; never substitute held-out clips to fill missing training inputs.

## Make the preservation NPZ for adaptation

`prepare_features.py` stores variable-length **uint16** frontend matrices in
RaggedMmap directories. The added exporter reads only their `training` splits:

```sh
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/make_public_npz.py \
  --features "$ORION_WORKSPACE/features-v2" \
  --output "$ORION_WORKSPACE/public-training.npz"
```

Output: `x` is uint16 `[N,220,40]`, `y` is binary `[N]`. Defaults select 1,000
positive and 3,000 negative windows, without replacement within each source
category. Positives keep the trailing phrase window; negatives use seeded random
windows. `adapt_local.py` converts these integers to floating frontend values with
`x * 0.0390625`. Do not pre-scale or feed validation/testing features into this
file. The script rejects incomplete or undersized inputs and existing output.

## Record, adapt and validate your own voice

Stop Orion and competing audio capture first. Stage `dotty-calibrate` as described
in the root `SETUP.md`. Obtain consent from everyone whose voice is recorded.
Run these from the computer, using a new private host directory:

```sh
mkdir -p "$ORION_WORKSPACE/private"
chmod 700 "$ORION_WORKSPACE/private"
adb shell '/data/local/tmp/dotty/dotty-calibrate -seconds 12 -out /data/local/tmp/dotty/calibration-train.s16le'
adb pull /data/local/tmp/dotty/calibration-train.s16le "$ORION_WORKSPACE/private/train.s16le"
adb shell '/data/local/tmp/dotty/dotty-calibrate -seconds 12 -out /data/local/tmp/dotty/calibration-heldout.s16le'
adb pull /data/local/tmp/dotty/calibration-heldout.s16le "$ORION_WORKSPACE/private/heldout.s16le"
chmod 600 "$ORION_WORKSPACE/private/"*.s16le
```

For each recording, unmute intentionally and press Action when READY appears.
White means recording; Mute cancels. Say “Hey Orion” three or four times with clear
silence between phrases. Use a **different** take for held-out evaluation, ideally
at another distance/time. Twelve seconds is a small calibration sample, not a
robustness guarantee. No network is used by calibration; output is raw 16 kHz,
mono, signed-16 little-endian PCM. It refuses to overwrite an existing clip.

Inspect the training clip locally in an audio editor (import raw with the format
above), or convert it to WAV using `ffmpeg -f s16le -ar 16000 -ac 1 -i INPUT OUTPUT.wav`.
Mark each complete phrase's start/end in seconds, including all phonemes but not
adjacent phrases. Supply at least two segments, each at most three seconds. The
following boundaries are **examples to replace**, not timings for your recording:

```sh
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/adapt_local.py \
  --clip "$ORION_WORKSPACE/private/train.s16le" \
  --segments '[[0.8,2.0],[3.5,4.7],[6.2,7.4]]' \
  --public-npz "$ORION_WORKSPACE/public-training.npz" \
  --checkpoint "$ORION_WORKSPACE/trained_models/candidate-v2" \
  --esc50 "$ORION_WORKSPACE/environmental/ESC-50-master" \
  --output "$ORION_WORKSPACE/private/adapted-v1"
```

The checkpoint directory must contain `training_config.yaml` and
`best_weights.weights.h5` from `train.py`. Adaptation writes private features,
weights and `model.tflite`; **it already performs streaming quantized export**.
Do not run the ordinary checkpoint exporter over this adapted directory.
Nothing is uploaded or deployed. If you mine public hard negatives, pass
`--revision v2` to `mine_local_negatives.py` to match the selected synthetic corpus,
then use its output with `adapt_local.py --hard-negatives PATH` on a new candidate:

```sh
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/mine_local_negatives.py \
  --workspace "$ORION_WORKSPACE" --revision v2 \
  --helper /path/to/host-helper --model "$ORION_WORKSPACE/export-v2" \
  --output "$ORION_WORKSPACE/private/hard-negatives-v2.npz"
```

The output must end in `.npz`; both that file and its JSON sidecar must be new.

Build a native host helper, then test the exported model with a candidate threshold:

```sh
python3 scripts/test-microwake.py /path/to/host-helper \
  "$ORION_WORKSPACE/private/adapted-v1" --threshold .98 \
  --positive "$ORION_WORKSPACE/private/heldout.s16le=3" \
  --negative /path/to/private/unrelated-speech.wav
```

Replace `3` with the actual number of phrases and `.98` with the candidate you
are evaluating. Test unrelated conversation, similar phrases and continuous
background noise as well as target speech. If you tune using a recording, it has
become validation data; reserve a fresh final test. Only after host and hardware
tests pass should you copy the chosen model to `wake/model.tflite` and configure
the matching threshold. All personal clips, feature arrays, weights and provenance
stay private. Delete or archive your calibration files on your own schedule;
the build tools do not automatically remove them. After verifying the pulled clips
have the expected size (12 × 32000 bytes here), can be read locally, and are stored
where you intend, you can remove the device copies explicitly:

```sh
adb shell 'rm /data/local/tmp/dotty/calibration-train.s16le /data/local/tmp/dotty/calibration-heldout.s16le'
```

Keep the private host copies needed for your training/evaluation.

## Tool checks

Run these from the repository root:

```sh
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/test_public_npz.py
"$ORION_WORKSPACE/train-env/bin/python" training/microwakeword/test_cli_inputs.py
```

The CLI tests reject missing evaluation groups and unsafe output destinations. It checks feature shape/scaling, deterministic
selection, exclusion of held-out splits and rejection of invalid input. Full GPU
training and model accuracy still require the separate validation steps above.
