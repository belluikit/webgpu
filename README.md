# WebGPU (belluikit fork)

This is [belluikit/webgpu](https://github.com/belluikit/webgpu), the WebGPU
bindings [Bellui](https://bellui.com) ([belluikit/bellui](https://github.com/belluikit/bellui))
renders through by default (`//go:build !gl`). It is a **modified version of
[oliverbestmann/webgpu](https://github.com/oliverbestmann/webgpu)**, which is the
maintained continuation of the `rajveermalviya/go-webgpu` →
`cogentcore/webgpu` lineage.

```sh
go get github.com/belluikit/webgpu
```

## What this fork changes

The fork exists to add two platform slices upstream does not ship, both of
which Bellui targets:

| Delta | Why |
|---|---|
| `libs-ios` branch: `arm64-simulator/`, behind the `iossimulator` build tag | Go builds device *and* Simulator as `GOOS=ios/GOARCH=arm64`, but their Mach-O platforms differ, so the device archive cannot link a Simulator binary. Upstream ships device arm64 + Intel-simulator amd64 only. The slice is the `ios-aarch64-simulator` build from the same wgpu-native release. |
| `libs-windows` branch: `arm64/libwgpu_native.a` built for the GNU (gnullvm) ABI | Upstream ships an aarch64-pc-windows-**MSVC** lib whose SEH/vcruntime C++ EH cannot be linked by the llvm-mingw toolchain Go/cgo uses on windows/arm64. Rebuilt by `.github/workflows/build-wgpu-windows-arm64.yml`. |

Because Go modules are fetched by module path, all five platform library modules
are served from this repository: `go.mod` requires
`github.com/belluikit/webgpu/libs-{android,darwin,ios,linux,windows}` from its
`libs-*` branches. The android, darwin and linux branches hold upstream's
wgpu-native archives unchanged; only their module paths and license notices
differ. The Go bindings differ from upstream only in the module path.

The full list of changes, and the copyright on them, is in [NOTICE](NOTICE).
This fork is licensed under the Apache License 2.0, like upstream ([LICENSE](LICENSE)).

---

wgpu-native version: v29.0.1.1 (the `VERSION` in `update-wgpu.sh`)

Go bindings for WebGPU, a cross-platform, safe graphics API.

It runs natively using [wgpu-native](https://github.com/gfx-rs/wgpu-native) on Vulkan, Metal, D3D12 and OpenGL ES. As
there is still no cgo for wasm, wasm/web builds with `GOOS=js` are using the browser WebGPU interface directly.

Upstream (oliverbestmann/webgpu) enhances the API by introducing garbage collection for types returned by WebGPU, preventing object leaks when
`Release()` is not called, thus mirroring browser/JavaScript WebGPU behavior.

For more information, see:

- [WebGPU](https://gpuweb.github.io/gpuweb/)
- [WGSL](https://gpuweb.github.io/gpuweb/wgsl/)
- [webgpu-native](https://github.com/webgpu-native/webgpu-headers)

The included static libraries downloaded from the wgpu-native project.

## Building

You might need to have dependencies installed. For debian/ubuntu, you can do
```sh
apt install -y libegl1-mesa-dev mesa-vulkan-drivers \
    libasound2-dev libxcursor-dev libxi-dev libxinerama-dev libxrandr-dev libxxf86vm-dev \
    libxkbcommon-dev libwayland-dev
```

## Error handling

Error handling in this library is intentionally designed to use panics for most WebGPU-related validation errors. This
decision is made to simplify GPU programming by immediately highlighting programming mistakes, similar to how `arr[idx]`
panics in Go. Many of these errors are validation-related and considered "programmer errors" rather than "expected" or
"user errors", where graceful error handling is less applicable. For example passing the wrong `TextureFormat` to a pipeline,
or setting the `SampleCount` of a texture. However, this approach does not affect methods that can
genuinely fail, such as `RequestAdapter`.

If maintaining panic-free code is essential for your needs, there exists a
`Try` variant for most methods, such as `TryWriteBuffer(...) error`, which allows you to handle errors without panics.

### WASM

Error handling in the browser's WebGPU implementation is often asynchronous. For instance, an invalid WGSL shader file will not immediately return an error or throw an exception from `CreateShaderModule`. Some errors do cause synchronous exceptions, such as missing required properties. This library attempts to address these by catching synchronous exceptions in `Try` methods, but some validation errors, like an invalid `usage` attribute for buffer creation, might be missed.

## Prebuild libraries

This repository uses prebuild libraries provided by `wgpu-native`. All libraries combined are more than 512mb in size,
which is more than `go get` allows in a single library. This is an "opinionated limit" by golang which is not configurable.

To work around that, libraries for the different systems are split into branches. An example is here:
https://github.com/belluikit/webgpu/tree/libs-linux/libs-linux

The `update-wgpu.sh` script updates all those branches and updates the go.mod file to pull the prebuild libraries as
dependencies in.

## Examples

You can find some examples in the examples directory: https://github.com/belluikit/webgpu/tree/main/examples

## Special thanks

This is a fork of [oliverbestmann/webgpu](https://github.com/oliverbestmann/webgpu), which forks
[cogentcore/webgpu](https://github.com/cogentcore/webgpu/), which in turn derives from
[rajveermalviya/go-webgpu](https://github.com/rajveermalviya/go-webgpu). Thanks to them for the work they put into
WebGPU for Go.
