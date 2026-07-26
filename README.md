Prebuilt wgpu libraries from wgpu-native, for iOS.

Fork of oliverbestmann/webgpu@libs-ios (wgpu-native v29.0.1.1), adding the
aarch64 iOS Simulator slice (`arm64-simulator/`, from the same release) behind
the `iossimulator` build tag — upstream ships device arm64 and Intel-simulator
amd64 only, so Apple-silicon Simulator builds could not link.
