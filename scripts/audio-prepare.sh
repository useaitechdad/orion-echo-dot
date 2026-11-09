#!/system/bin/sh
# Verified DAC routes plus measured I2S On correction (the tested firmware).
# Capture audio-snapshot.sh first. Run with the existing Dotty daemon stopped.
# This changes runtime mixer state only; it does not edit init/boot/system files.
set -eu
mix() {
    echo "tinymix: $*"
    tinymix "$@"
}
mix 'MFP Gpio Mute' Off
mix 'Ext_Speaker_Amp_Switch' Off
mix 'Audio_I2S1_Setting' On
mix 'Audio_DacMux_Setting' Off
mix 'Headphone_Speaker_Mux' Speaker
mix 'Ext_Amp_Gain' 6dB
mix 'HP DAC Playback Switch' 1 1
mix 'HPR Output Mixer R_DAC Switch' 1
mix 'HPL Output Mixer L_DAC Switch' 1
mix 'Right Channel Only' On
mix 'Ignore Ramp Up' On
mix 'PCM Playback Volume' 127 127
mix 'HP Driver Gain Volume' 6 6
mix 'Auto-mute Switch' 3
echo 'Verified routing applied; amp remains off until playback.'
