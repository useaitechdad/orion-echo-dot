// Local-only microWakeWord helper. Same framed PCM protocol as native/wake.
// No capture, networking, credentials or audio recording. stdout is protocol only.
#include "tflite_abi.h"
extern "C" {
#include "tensorflow/lite/experimental/microfrontend/lib/frontend.h"
#include "tensorflow/lite/experimental/microfrontend/lib/frontend_util.h"
}
#include <algorithm>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

class Detector {
 public:
  ~Detector() {
    if (interpreter_) TfLiteInterpreterDelete(interpreter_);
    if (model_) TfLiteModelDelete(model_);
    if (frontend_ready_) FrontendFreeStateContents(&frontend_);
  }
  bool open(const char *directory, float threshold) {
    threshold_ = threshold;
    const std::string path = std::string(directory) + "/model.tflite";
    model_ = TfLiteModelCreateFromFile(path.c_str());
    if (!model_) return false;
    // Match pymicro-features 2.0.2 / the training frontend exactly.
    config_.window.size_ms = 30;
    config_.window.step_size_ms = 10;
    config_.filterbank.num_channels = 40;
    config_.filterbank.lower_band_limit = 125;
    config_.filterbank.upper_band_limit = 7500;
    config_.noise_reduction.smoothing_bits = 10;
    config_.noise_reduction.even_smoothing = .025f;
    config_.noise_reduction.odd_smoothing = .06f;
    config_.noise_reduction.min_signal_remaining = .05f;
    config_.pcan_gain_control.enable_pcan = 1;
    config_.pcan_gain_control.strength = .95f;
    config_.pcan_gain_control.offset = 80;
    config_.pcan_gain_control.gain_bits = 21;
    config_.log_scale.enable_log = 1;
    config_.log_scale.scale_shift = 6;
    return reset();
  }
  bool reset() {
    if (frontend_ready_) {
      FrontendFreeStateContents(&frontend_);
      frontend_ready_ = false;
    }
    frontend_ = {};
    if (!FrontendPopulateState(&config_, &frontend_, 16000)) return false;
    frontend_ready_ = true;
    if (interpreter_) TfLiteInterpreterDelete(interpreter_);
    interpreter_ = nullptr;
    auto options = TfLiteInterpreterOptionsCreate();
    if (!options) return false;
    TfLiteInterpreterOptionsSetNumThreads(options, 1);
    interpreter_ = TfLiteInterpreterCreate(model_, options);
    TfLiteInterpreterOptionsDelete(options);
    if (!interpreter_ || TfLiteInterpreterAllocateTensors(interpreter_) != 0) return false;
    if (TfLiteInterpreterGetInputTensorCount(interpreter_) != 1 ||
        TfLiteInterpreterGetOutputTensorCount(interpreter_) != 1) return false;
    input_ = TfLiteInterpreterGetInputTensor(interpreter_, 0);
    output_ = TfLiteInterpreterGetOutputTensor(interpreter_, 0);
    if (!input_ || !output_ || TfLiteTensorNumDims(input_) != 3 ||
        TfLiteTensorDim(input_, 0) != 1 || TfLiteTensorDim(input_, 2) != 40) return false;
    stride_ = TfLiteTensorDim(input_, 1);
    if (stride_ < 1 || stride_ > 10 || TfLiteTensorByteSize(input_) != size_t(stride_ * 40) ||
        TfLiteTensorByteSize(output_) != 1) return false;
    input_type_ = TfLiteTensorType(input_);
    output_type_ = TfLiteTensorType(output_);
    if ((input_type_ != 9 && input_type_ != 3) || (output_type_ != 9 && output_type_ != 3)) return false;
    input_q_ = TfLiteTensorQuantizationParams(input_);
    output_q_ = TfLiteTensorQuantizationParams(output_);
    if (!std::isfinite(input_q_.scale) || input_q_.scale <= 0 ||
        !std::isfinite(output_q_.scale) || output_q_.scale <= 0) return false;
    features_.clear();
    pending_ = 0;
    probability_count_ = probability_next_ = 0;
    armed_ = true;
    return true;
  }
  bool process(const unsigned char *bytes, size_t len, bool &found) {
    found = false;
    for (size_t i = 0; i < len; i += 2) {
      samples_[pending_++] = int16_t(uint16_t(bytes[i]) | (uint16_t(bytes[i+1]) << 8));
      if (pending_ != 160) continue;
      pending_ = 0;
      size_t consumed = 0;
      const auto f = FrontendProcessSamples(&frontend_, samples_, 160, &consumed);
      if (consumed != 160 || (f.size != 0 && f.size != 40)) return false;
      for (size_t j = 0; j < f.size; ++j) {
        // nearest-even rounding matches numpy.round; saturate rather than wrap.
        float value = std::nearbyint(f.values[j] * .0390625f / input_q_.scale + input_q_.zero_point);
        const int lo = input_type_ == 9 ? -128 : 0;
        const int hi = input_type_ == 9 ? 127 : 255;
        features_.push_back(uint8_t(int(std::max(float(lo), std::min(float(hi), value)))));
      }
      if (features_.size() != size_t(stride_ * 40)) continue;
      if (TfLiteTensorCopyFromBuffer(input_, features_.data(), features_.size()) != 0 ||
          TfLiteInterpreterInvoke(interpreter_) != 0) return false;
      features_.clear();
      uint8_t raw = 0;
      if (TfLiteTensorCopyToBuffer(output_, &raw, 1) != 0) return false;
      const int q = output_type_ == 9 ? int(int8_t(raw)) : int(raw);
      const float probability = (q - output_q_.zero_point) * output_q_.scale;
      probabilities_[probability_next_] = probability;
      probability_next_ = (probability_next_ + 1) % 3;
      probability_count_ = std::min(3, probability_count_ + 1);
      if (probability_count_ < 3) continue;
      const float mean = (probabilities_[0] + probabilities_[1] + probabilities_[2]) / 3;
      if (std::getenv("ORION_WAKE_SCORES")) std::fprintf(stderr, "wake_probability=%.6f\n", mean);
      if (armed_ && mean > threshold_) {
        found = true;
        armed_ = false;
      }
      // One trigger per high-probability region; explicit Reset also rearms.
      if (mean < threshold_ * .5f) armed_ = true;
    }
    return true;
  }
 private:
  TfLiteModel *model_ = nullptr;
  TfLiteInterpreter *interpreter_ = nullptr;
  TfLiteTensor *input_ = nullptr;
  const TfLiteTensor *output_ = nullptr;
  struct TfLiteQuantizationParams input_q_ {}, output_q_ {};
  FrontendConfig config_ {};
  FrontendState frontend_ {};
  bool frontend_ready_ = false, armed_ = true;
  int stride_ = 0, input_type_ = 0, output_type_ = 0;
  size_t pending_ = 0;
  int16_t samples_[160] {};
  std::vector<uint8_t> features_;
  float threshold_ = .9f, probabilities_[3] {};
  int probability_count_ = 0, probability_next_ = 0;
};

int main(int argc, char **argv) {
  if (argc != 3) {
    std::fprintf(stderr, "usage: dotty-microwake model-directory probability-threshold\n");
    return 2;
  }
  char *end;
  const float threshold = std::strtof(argv[2], &end);
  // Reject an accidentally inherited PocketSphinx likelihood threshold.
  if (end == argv[2] || *end || !std::isfinite(threshold) || threshold < .5f || threshold >= 1) {
    std::fprintf(stderr, "microWakeWord threshold must be finite and in [0.5, 1); PocketSphinx thresholds are invalid\n");
    return 2;
  }
  Detector detector;
  if (!detector.open(argv[1], threshold)) {
    std::fprintf(stderr, "microWakeWord model/frontend initialization failed\n");
    return 1;
  }
  std::setbuf(stdout, nullptr);
  std::putchar('R');
  unsigned char header[2], bytes[4096];
  for (;;) {
    const size_t n = std::fread(header, 1, 2, stdin);
    if (n == 0 && std::feof(stdin)) return 0;
    if (n != 2) return 2;
    const size_t len = header[0] | size_t(header[1]) << 8;
    if (len > sizeof(bytes) || len % 2) return 2;
    if (len == 0) {
      if (!detector.reset()) return 1;
      std::putchar('0');
      continue;
    }
    if (std::fread(bytes, 1, len, stdin) != len) return 2;
    bool found;
    if (!detector.process(bytes, len, found)) return 1;
    std::putchar(found ? '1' : '0');
  }
}
