// Copyright The TensorFlow Authors. Licensed under Apache-2.0.
// Minimal declarations from TensorFlow Lite 2.17.1's stable C API.
// Upstream: tensorflow/lite/core/c/c_api.h and c_api_types.h (Apache-2.0).
// Opaque handles avoid depending on TensorFlow's internal C++ ABI.
#pragma once
#include <stddef.h>
#include <stdint.h>
extern "C" {
struct TfLiteModel;
struct TfLiteInterpreter;
struct TfLiteInterpreterOptions;
struct TfLiteTensor;
struct TfLiteQuantizationParams { float scale; int32_t zero_point; };
const char *TfLiteVersion();
TfLiteModel *TfLiteModelCreateFromFile(const char *);
void TfLiteModelDelete(TfLiteModel *);
TfLiteInterpreterOptions *TfLiteInterpreterOptionsCreate();
void TfLiteInterpreterOptionsDelete(TfLiteInterpreterOptions *);
void TfLiteInterpreterOptionsSetNumThreads(TfLiteInterpreterOptions *, int32_t);
TfLiteInterpreter *TfLiteInterpreterCreate(const TfLiteModel *, const TfLiteInterpreterOptions *);
void TfLiteInterpreterDelete(TfLiteInterpreter *);
int TfLiteInterpreterAllocateTensors(TfLiteInterpreter *);
int TfLiteInterpreterInvoke(TfLiteInterpreter *);
int32_t TfLiteInterpreterGetInputTensorCount(const TfLiteInterpreter *);
int32_t TfLiteInterpreterGetOutputTensorCount(const TfLiteInterpreter *);
TfLiteTensor *TfLiteInterpreterGetInputTensor(const TfLiteInterpreter *, int32_t);
const TfLiteTensor *TfLiteInterpreterGetOutputTensor(const TfLiteInterpreter *, int32_t);
int TfLiteTensorType(const TfLiteTensor *);
int32_t TfLiteTensorNumDims(const TfLiteTensor *);
int32_t TfLiteTensorDim(const TfLiteTensor *, int32_t);
size_t TfLiteTensorByteSize(const TfLiteTensor *);
TfLiteQuantizationParams TfLiteTensorQuantizationParams(const TfLiteTensor *);
int TfLiteTensorCopyFromBuffer(TfLiteTensor *, const void *, size_t);
int TfLiteTensorCopyToBuffer(const TfLiteTensor *, void *, size_t);
}
