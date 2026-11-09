#!/usr/bin/env python3
"""Export a completed checkpoint without altering its training artifacts."""
import argparse
import json
from pathlib import Path
import yaml


def main():
    p = argparse.ArgumentParser()
    p.add_argument('checkpoint', type=Path)
    p.add_argument('output', type=Path)
    p.add_argument('--weights', default='best_weights')
    args = p.parse_args()
    if args.output.exists():
        p.error('export output already exists; select a new destination')
    import tensorflow as tf
    from microwakeword import mixednet, utils, data
    from microwakeword.layers import modes
    tf.keras.utils.set_random_seed(6106)
    # Upstream serializes training_input_shape with !!python/tuple. FullLoader
    # supports that basic type without enabling arbitrary Python object loads.
    config = yaml.full_load((args.checkpoint / 'training_config.yaml').read_text())
    flags = argparse.Namespace(**config['flags'])
    model = mixednet.model(flags, shape=config['training_input_shape'], batch_size=1)
    model.load_weights(str(args.checkpoint / (args.weights + '.weights.h5')))
    args.output.mkdir(parents=True)
    config['train_dir'] = str(args.output.resolve())
    source = args.output / 'stream_state_internal'
    utils.convert_model_saved(model, config, folder='stream_state_internal',
                              mode=modes.Modes.STREAM_INTERNAL_STATE_INFERENCE)
    processor = data.FeatureHandler(config)
    # Quantization feeds streaming chunks, not the training input tensor. Drop
    # at most stride-1 frames from calibration windows to form complete chunks.
    # The trained architecture, weights and streaming model are unchanged.
    calibration = dict(config)
    calibration['spectrogram_length'] -= calibration['spectrogram_length'] % config['stride']
    utils.convert_saved_model_to_tflite(calibration, audio_processor=processor,
        path_to_model=str(source), folder=str(args.output), fname='model.tflite', quantize=True)
    (args.output / 'provenance.json').write_text(json.dumps({
        'checkpoint': args.checkpoint.name, 'weights': args.weights,
        'frame_step_ms': 10, 'streaming': True, 'quantized': True,
        'calibration_frames': calibration['spectrogram_length'],
        'status': 'candidate; validation required',
    }, indent=2) + '\n')


if __name__ == '__main__':
    main()
