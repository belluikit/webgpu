//go:build !js

package wgpu

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDeviceGetLimitsReadsNativeLimits guards #8: Device.GetLimits must chain
// the WGPUNativeLimits struct itself, not the address of the Go variable that
// points at it, or wgpu-native never fills the native fields (and reads a
// garbage sType out of Go memory); both getters must also tag the chain with
// WGPUSType_NativeLimits, or wgpu-native ignores it. An unfilled native field
// reads 0. (The device need not echo the adapter's value: on Metal the adapter
// reports u32 max for maxNonSamplerBindings and the device 1,000,000.)
func TestDeviceGetLimitsReadsNativeLimits(t *testing.T) {
	instance := CreateInstance(nil)
	defer instance.Release()
	adapter, err := instance.RequestAdapter(nil)
	if err != nil {
		t.Skipf("no WebGPU adapter available: %v", err)
	}
	defer adapter.Release()

	want := adapter.GetLimits()
	require.NotZero(t, want.MaxNonSamplerBindings, "adapter reports no native limits")

	device, err := adapter.RequestDevice(&DeviceDescriptor{RequiredLimits: &want})
	require.NoError(t, err)
	defer device.Release()

	got := device.GetLimits()
	require.Equal(t, want.MaxTextureDimension2D, got.MaxTextureDimension2D)
	require.NotZero(t, got.MaxNonSamplerBindings, "device native limits were not filled")
	require.LessOrEqual(t, got.MaxNonSamplerBindings, want.MaxNonSamplerBindings)
}
