//go:build !js

package wgpu

/*

#include <string.h>
#include <wgpu.h>
#include "device_lost.h"

void gowebgpu_device_lost_record_c(WGPUDevice const * device, WGPUDeviceLostReason reason, WGPUStringView message, void * userdata1, void * userdata2) {
  gowebgpu_device_lost *l = userdata1;
  size_t n = 0;
  if (message.data != NULL) {
    n = message.length == WGPU_STRLEN ? strlen(message.data) : message.length;
  }
  if (n > sizeof(l->message) - 1) {
    n = sizeof(l->message) - 1;
  }
  if (n > 0) {
    memcpy(l->message, message.data, n);
  }
  l->message[n] = 0;
  l->reason = reason;
  __atomic_store_n(&l->lost, 1, __ATOMIC_RELEASE);
}

int gowebgpu_device_lost_get(gowebgpu_device_lost *l) {
  return __atomic_load_n(&l->lost, __ATOMIC_ACQUIRE);
}

void gowebgpu_buffer_map_callback_c(WGPUMapAsyncStatus status, WGPUStringView message, void * userdata, void * userdata2) {
  extern void gowebgpu_buffer_map_callback_go(WGPUMapAsyncStatus status, void * userdata);
  gowebgpu_buffer_map_callback_go(status, userdata);
}

void gowebgpu_request_adapter_callback_c(WGPURequestAdapterStatus status, WGPUAdapter adapter, WGPUStringView message, void * userdata1, void * userdata2) {
  extern void gowebgpu_request_adapter_callback_go(WGPURequestAdapterStatus status, WGPUAdapter adapter, WGPUStringView message, void * userdata);
  gowebgpu_request_adapter_callback_go(status, adapter, message, userdata1);
}

void gowebgpu_request_device_callback_c(WGPURequestDeviceStatus status, WGPUDevice device, WGPUStringView message, void * userdata1, void * userdata2) {
  extern void gowebgpu_request_device_callback_go(WGPURequestDeviceStatus status, WGPUDevice device, WGPUStringView message, void * userdata);
  gowebgpu_request_device_callback_go(status, device, message, userdata1);
}

void gowebgpu_queue_work_done_callback_c(WGPUQueueWorkDoneStatus status, WGPUStringView message, void * userdata) {
  extern void gowebgpu_queue_work_done_callback_go(WGPUQueueWorkDoneStatus status, WGPUStringView message, void * userdata);
  gowebgpu_queue_work_done_callback_go(status, message, userdata);
}

*/
import "C"
