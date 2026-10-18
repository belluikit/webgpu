//go:build darwin

package wgpuglfw

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/belluikit/webgpu/wgpu"
)

/*

#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore

#import <Cocoa/Cocoa.h>
#import <QuartzCore/CAMetalLayer.h>

// Takes and returns void*, not CFTypeRef: cgo maps CFTypeRef to uintptr, and
// converting that back to unsafe.Pointer in Go is what go vet flags.
void *metalLayerFromNSWindow(void *nsWindowRef) {
	NSWindow *ns_window = (__bridge NSWindow *)nsWindowRef;
	id metal_layer = NULL;
	[ns_window.contentView setWantsLayer:YES];
	metal_layer = [CAMetalLayer layer];
	[ns_window.contentView setLayer:metal_layer];
	return (__bridge void *)metal_layer;
}

*/
import "C"

func GetSurfaceDescriptor(w *glfw.Window) *wgpu.SurfaceDescriptor {
	return &wgpu.SurfaceDescriptor{
		MetalLayer: &wgpu.SurfaceSourceMetalLayer{
			Layer: C.metalLayerFromNSWindow(unsafe.Pointer(w.GetCocoaWindow())),
		},
	}
}
