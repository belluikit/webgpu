#pragma once

#include <wgpu.h>

// gowebgpu_device_lost records a device's loss in C memory. wgpu reports a
// loss from inside a later call on the device, and most calls are marked
// #cgo nocallback, so the report cannot call back into Go: Device.Lost
// reads the record instead.
typedef struct {
  int lost;
  WGPUDeviceLostReason reason;
  char message[256];
} gowebgpu_device_lost;

void gowebgpu_device_lost_record_c(WGPUDevice const * device, WGPUDeviceLostReason reason, WGPUStringView message, void * userdata1, void * userdata2);
int gowebgpu_device_lost_get(gowebgpu_device_lost *l);
