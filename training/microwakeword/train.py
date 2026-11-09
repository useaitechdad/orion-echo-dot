#!/usr/bin/env python3
"""Train one isolated candidate using prepared public/synthetic data."""
import argparse
import json
import os
from pathlib import Path
import runpy
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--workspace', type=Path, required=True)
    parser.add_argument('--name', default='hey-orion-v1')
    parser.add_argument('--steps', type=int, default=8000)
    parser.add_argument('--features', default='features')
    args = parser.parse_args()
    root = args.workspace.resolve()
    os.chdir(root)
    destination = root / 'trained_models' / args.name
    if destination.exists():
        parser.error('candidate already exists; preserve it and select a new name')
    features = []
    for kind, weight, penalty, truth in [
        ('positive', 3., 1., True), ('hard_negative', 2., 2., False),
        ('environment', 3., 1., False), ('speech', 4., 1., False),
    ]:
        for split in ['training', 'validation', 'testing']:
            if not (root / args.features / kind / split / 'complete.json').exists():
                parser.error(f'incomplete features: {kind}/{split}')
        features.append(dict(features_dir=str(root / args.features / kind),
                             sampling_weight=weight, penalty_weight=penalty,
                             truth=truth, truncation_strategy='truncate_start' if truth else 'random',
                             type='mmap'))
    warm = min(2000, args.steps // 4)
    finish = min(2000, args.steps // 4)
    config = dict(
        window_step_ms=10, train_dir=str(destination), features=features,
        training_steps=[warm, args.steps-warm-finish, finish],
        positive_class_weight=[1, 1, 1], negative_class_weight=[1, 5, 10],
        learning_rates=[.001, .0005, .0001], batch_size=128,
        time_mask_max_size=[5, 5, 5], time_mask_count=[1, 1, 1],
        freq_mask_max_size=[3, 3, 3], freq_mask_count=[1, 1, 1],
        eval_step_interval=min(1000, args.steps), clip_duration_ms=2000,
        target_minimization=.5, minimization_metric='ambient_false_positives_per_hour',
        maximization_metric='average_viable_recall',
    )
    config_path = root / (args.name + '-config.json')
    if config_path.exists():
        parser.error('config already exists; use a new candidate name')
    config_path.write_text(json.dumps(config, indent=2) + '\n')
    import tensorflow as tf
    tf.keras.utils.set_random_seed(6106)
    if not tf.config.list_physical_devices('GPU'):
        raise RuntimeError('Expected GPU unavailable; refusing an unnoticed CPU training run')
    sys.argv = ['microwakeword.model_train_eval', '--training_config=' + str(config_path),
                '--train', '1', '--restore_checkpoint', '0',
                '--test_tf_nonstreaming', '0', '--test_tflite_nonstreaming', '0',
                '--test_tflite_nonstreaming_quantized', '0', '--test_tflite_streaming', '0',
                '--test_tflite_streaming_quantized', '0', '--use_weights', 'best_weights',
                'mixednet', '--pointwise_filters', '64,64,64,64',
                '--repeat_in_block', '1,1,1,1', '--mixconv_kernel_sizes', '[5], [7,11], [9,15], [23]',
                '--residual_connection', '0,0,0,0', '--first_conv_filters', '32',
                '--first_conv_kernel_size', '5', '--stride', '3']
    runpy.run_module('microwakeword.model_train_eval', run_name='__main__')


if __name__ == '__main__':
    main()
