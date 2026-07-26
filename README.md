Prebuilt wgpu libraries from wgpu-native, for Windows.

Fork of oliverbestmann/webgpu@libs-windows (wgpu-native v29.0.1.1).

## arm64 is rebuilt, not vendored

Upstream ships an `aarch64-pc-windows-msvc` lib. Its SEH/vcruntime C++ EH
(`__CxxFrameHandler`, `??_7type_info@@6B@`) cannot be linked by the llvm-mingw
toolchain Go/cgo uses on windows/arm64, so it is unusable for a Go build — the
same reason amd64 uses the `-gnu` target rather than the MSVC one.

`arm64/libwgpu_native.a` is therefore built from wgpu-native v29.0.1.1 sources for
`aarch64-pc-windows-gnullvm` by the `build-wgpu-windows-arm64.yml` workflow in the
main branch of this repo, which also asserts the archive carries no MSVC C++ ABI
symbols. Regenerate it whenever the pinned wgpu-native release moves.
