package lib

/*
#cgo ios,amd64 LDFLAGS: -L${SRCDIR}/amd64 -lwgpu_native

// Go builds both iOS device and iOS Simulator binaries as GOOS=ios/GOARCH=arm64,
// but their Mach-O platforms differ (device vs PLATFORM_IOSSIMULATOR), so one
// archive cannot serve both. Device is the default; pass the `iossimulator` build
// tag for an Apple-silicon Simulator build:
//
//	go build -tags iossimulator
//
// amd64 iOS is always the (Intel) Simulator, so it needs no tag. The simulator
// slice comes from the same wgpu-native release as the device ones, so it matches
// the headers vendored in the parent module.
#cgo ios,arm64,!iossimulator LDFLAGS: -L${SRCDIR}/arm64 -lwgpu_native
#cgo ios,arm64,iossimulator LDFLAGS: -L${SRCDIR}/arm64-simulator -lwgpu_native
*/
import "C"
