//go:build !js

package wgpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeviceLost pins the device-lost report. RequestDevice refused a
// DeviceLostCallback (a Go pointer inside a Go-allocated descriptor), the C
// shim's signature did not match webgpu.h's, and wgpu reports a loss from
// inside a later call, most of which are #cgo nocallback, so a Go callback
// aborted the process. The loss is now recorded in C and read by Lost.
func TestDeviceLost(t *testing.T) {
	instance := CreateInstance(nil)
	defer instance.Release()
	adapter, err := instance.RequestAdapter(nil)
	require.NoError(t, err)
	defer adapter.Release()

	var told []DeviceLostReason
	device, err := adapter.RequestDevice(&DeviceDescriptor{
		DeviceLostCallback: func(reason DeviceLostReason, _ string) { told = append(told, reason) },
	})
	require.NoError(t, err)
	defer device.Release()

	_, _, lost := device.Lost()
	assert.False(t, lost, "a new device")

	device.Destroy()
	// wgpu reports the loss from inside the next call on the device.
	if enc, err := device.TryCreateCommandEncoder(nil); err == nil {
		enc.Release()
	}
	reason, message, lost := device.Lost()
	assert.True(t, lost, "after Destroy and a call")
	assert.Equal(t, DeviceLostReasonDestroyed, reason)
	assert.NotEmpty(t, message)

	device.Lost()
	assert.Equal(t, []DeviceLostReason{DeviceLostReasonDestroyed}, told, "the callback runs once")
}

// TestDeviceLostWithoutCallback: a device made with no descriptor records its
// loss too.
func TestDeviceLostWithoutCallback(t *testing.T) {
	instance := CreateInstance(nil)
	defer instance.Release()
	adapter, err := instance.RequestAdapter(nil)
	require.NoError(t, err)
	defer adapter.Release()
	device, err := adapter.RequestDevice(nil)
	require.NoError(t, err)
	defer device.Release()

	device.Destroy()
	if enc, err := device.TryCreateCommandEncoder(nil); err == nil {
		enc.Release()
	}
	_, _, lost := device.Lost()
	assert.True(t, lost)
}
