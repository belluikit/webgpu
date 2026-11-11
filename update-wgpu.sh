#!/usr/bin/env bash

set -ex -o pipefail

# gets the prebuilt release files from wgpu-native and installs
# the header files to wgpu/lib.
# the library files are pushed to branches according to the arch.
#
# belluikit fork, on top of upstream's script:
# - the iOS arm64 Simulator slice is vendored too (libs-ios/arm64-simulator);
# - windows/arm64 is a GNU-ABI build of the same release, made by
#   .github/workflows/build-wgpu-windows-arm64.yml, which this script
#   dispatches (upstream's slice is MSVC, which cgo cannot link);
# - every library goes through cmd/striplibs, which removes the sections
#   Go 1.27.4's cgo check refuses (#24).
# See NOTICE.
#
# Needs: gh (logged in), go, llvm-objcopy (LLVM_OBJCOPY overrides it).
#
# Environment:
#   LIBS_BRANCH_PREFIX  push each libs-<os> branch as <prefix>libs-<os>, for
#                       review, instead of updating libs-<os> itself; go.mod
#                       is pinned to the pushed commits either way.
#   WINDOWS_ARM64_RUN   use the artifact of this finished workflow run
#                       instead of dispatching a new one.
#   WORKFLOW_REF        branch to dispatch the workflow on (default: the
#                       current branch, which must be pushed).

VERSION="v29.0.1.1"

ZIPS="releases-$VERSION"

SYSTEMS="android darwin ios linux windows"

REPO="belluikit/webgpu"

LLVM_OBJCOPY="${LLVM_OBJCOPY:-llvm-objcopy}"

# download files and extract zip files.
# the resulting files are in $ZIPS/zips/<name>/, where <name> is the zip's
# name without "wgpu-" and "-release.zip", e.g. ios-aarch64-simulator.
function download-and-extract {
  if ! [[ -f $ZIPS/ok ]] ; then
    mkdir -p $ZIPS

    pushd $ZIPS
      rm -rf wgpu-native zips
      mkdir zips

      git clone https://github.com/gfx-rs/wgpu-native wgpu-native
      pushd wgpu-native
        gh release download $VERSION --dir ../zips -p "*-release.zip"
      popd

      pushd zips

        for ZIP in *.zip ; do
          ARCH="${ZIP#wgpu-}"
          ARCH="${ARCH%-release.zip}"
          # MSVC builds cgo cannot link; windows/arm64 is built by the
          # workflow instead (fetch-windows-arm64), and windows/386 is not
          # supported (#25).
          if [[ "$ARCH" = windows-*-msvc ]] ; then
            continue
          fi

          mkdir "$ARCH"
          pushd "$ARCH"
            unzip ../"$ZIP"
          popd
        done
      popd
    popd

    touch $ZIPS/ok
  fi
}

# put the GNU-ABI windows/arm64 build of $VERSION in
# $ZIPS/zips/windows-aarch64-gnullvm/lib, dispatching the workflow unless
# WINDOWS_ARM64_RUN names a finished run.
function fetch-windows-arm64 {
  local DIR="$ZIPS/zips/windows-aarch64-gnullvm/lib"
  if ! [[ -f "$DIR/libwgpu_native.a" ]] ; then
    local RUN="${WINDOWS_ARM64_RUN:-}"
    if [[ -z "$RUN" ]] ; then
      local REF="${WORKFLOW_REF:-$(git branch --show-current)}"
      local URL
      URL=$(gh workflow run build-wgpu-windows-arm64.yml -R "$REPO" --ref "$REF" -f wgpu_native_ref="$VERSION" | tail -1)
      RUN="${URL##*/}"
      if ! [[ "$RUN" =~ ^[0-9]+$ ]] ; then
        echo "could not read the dispatched run from: $URL" >&2
        exit 1
      fi
      gh run watch "$RUN" -R "$REPO" --exit-status
    fi

    rm -rf "$DIR"
    mkdir -p "$DIR"
    gh run download "$RUN" -R "$REPO" -n wgpu-windows-arm64-gnullvm -D "$DIR"
  fi

  local BUILT
  BUILT=$(cat "$DIR/WGPU_NATIVE_REF")
  if [[ "$BUILT" != "$VERSION" ]] ; then
    echo "windows/arm64 build is of $BUILT, not $VERSION" >&2
    exit 1
  fi
}

# create the libs repository and fetch latest branches
function fetch-libs-repository {
  mkdir -p libs

  pushd libs
    if ! [ -d repo/.git ] ; then
      mkdir repo
      pushd repo
        git init .
        git remote add origin https://github.com/$REPO
      popd
    fi

    for SYSTEM in $SYSTEMS ; do
      git -C repo fetch origin libs-"$SYSTEM"
      git -C repo switch -C libs-"$SYSTEM" origin/libs-"$SYSTEM"
    done
  popd
}

# push all library branches to the git repository
function push-libs-repository {
  for SYSTEM in $SYSTEMS ; do
    git -C libs/repo push origin libs-"$SYSTEM":refs/heads/"${LIBS_BRANCH_PREFIX:-}"libs-"$SYSTEM"
  done
}

# copy a zip's headers to a directory under wgpu/lib, with LF line endings:
# the windows zips ship them with CRLF, the others with LF.
function copy-headers() {
  local ZIP="$1"
  local DIR="$2"
  local H
  mkdir -p "$DIR"
  for H in $ZIPS/zips/"$ZIP"/include/webgpu/*.h ; do
    sed 's/\r$//' "$H" > "$DIR/$(basename "$H")"
  done
  git add "$DIR"
}

# copy files into target directories. HEADERS=no skips the headers, for a
# slice whose arch shares a header directory with another.
function copy-to-target() {
  local ZIP="$1"
  local TARGET="$2"
  local ARCH="$3"
  local LIB="$4"
  local HEADERS="${5:-yes}"

  # copy headers
  if [[ "$HEADERS" = yes ]] ; then
    copy-headers "$ZIP" "wgpu/lib/$TARGET/$ARCH"
  fi

  # switch to the correct library branch
  git -C libs/repo switch "libs-$TARGET"

  # copy lib to branch, without the sections cgo refuses
  mkdir -p libs/repo/libs-"$TARGET"/"$ARCH"
  cp $ZIPS/zips/"$ZIP"/lib/"$LIB" libs/repo/libs-"$TARGET"/"$ARCH"
  $ZIPS/striplibs -objcopy "$LLVM_OBJCOPY" libs/repo/libs-"$TARGET"/"$ARCH"/"$LIB"
  git -C libs/repo add libs-"$TARGET"/"$ARCH"

  # commit the file if something has changed
  if [ -n "$(git -C libs/repo status --porcelain)" ] ; then
    git -C libs/repo commit -m "update library to $VERSION"
  fi
}

# Rust target triples each libs-* branch's libraries are built for.
function rust-targets() {
  case "$1" in
    android) echo aarch64-linux-android,x86_64-linux-android,armv7-linux-androideabi,i686-linux-android ;;
    darwin)  echo aarch64-apple-darwin,x86_64-apple-darwin ;;
    ios)     echo aarch64-apple-ios,x86_64-apple-ios,aarch64-apple-ios-sim ;;
    linux)   echo aarch64-unknown-linux-gnu,x86_64-unknown-linux-gnu ;;
    windows) echo x86_64-pc-windows-gnu,aarch64-pc-windows-gnullvm ;;
  esac
}

# write libs-*/THIRD_PARTY_NOTICES.txt on each branch (#17): the Rust
# packages compiled into its libraries and the notices of the Rust
# standard library of each rustc that built one. The rustc versions are
# read from the libraries themselves, so the hand-built slices count too.
function write-notices() {
  local ROOT
  ROOT=$(pwd)
  git -C "$ZIPS/wgpu-native" fetch -q --tags
  git -C "$ZIPS/wgpu-native" checkout -q "$VERSION"

  for SYSTEM in $SYSTEMS ; do
    git -C libs/repo switch "libs-$SYSTEM"
    local DIR="libs/repo/libs-$SYSTEM"
    local STDS=""
    local V
    for V in $(find "$DIR" \( -name '*.a' -o -name '*.lib' \) -exec strings {} \; \
        | sed -nE 's/.*rustc version ([0-9]+\.[0-9]+\.[0-9]+) .*/\1/p' | sort -u) ; do
      local HTML="$ROOT/$ZIPS/rustc-$V/COPYRIGHT-library.html"
      if ! [[ -f "$HTML" ]] ; then
        mkdir -p "$ZIPS/rustc-$V"
        curl -sfL "https://static.rust-lang.org/dist/rustc-$V-x86_64-unknown-linux-gnu.tar.xz" \
          | tar -xJf - -C "$ZIPS/rustc-$V" --strip-components 5 \
              "rustc-$V-x86_64-unknown-linux-gnu/rustc/share/doc/rust/COPYRIGHT-library.html"
      fi
      STDS="${STDS:+$STDS,}$V=$HTML"
    done
    if [[ -z "$STDS" ]] ; then
      echo "no rustc version found in $DIR's libraries" >&2
      exit 1
    fi

    (cd cmd/notices && go run . -src "$ROOT/$ZIPS/wgpu-native" -targets "$(rust-targets "$SYSTEM")" \
      -std "$STDS" -o "$ROOT/$DIR/THIRD_PARTY_NOTICES.txt")
    git -C libs/repo add "libs-$SYSTEM/THIRD_PARTY_NOTICES.txt"
    if [ -n "$(git -C libs/repo status --porcelain)" ] ; then
      git -C libs/repo commit -m "third-party notices for $VERSION"
    fi
  done
}

function write-gitattributes() {
  cat > wgpu/lib/.gitattributes <<EOF2
# See
# https://github.com/github/linguist/blob/249bbd1c2ffc631ca2ec628da26be5800eec3d48/docs/overrides.md#vendored-code

webgpu.h linguist-vendored
wgpu.h linguist-vendored
EOF2
}

download-and-extract

fetch-windows-arm64

go build -o $ZIPS/striplibs ./cmd/striplibs

fetch-libs-repository

copy-to-target "macos-aarch64"           "darwin"  "arm64"           libwgpu_native.a
copy-to-target "macos-x86_64"            "darwin"  "amd64"           libwgpu_native.a
copy-to-target "ios-aarch64"             "ios"     "arm64"           libwgpu_native.a
copy-to-target "ios-aarch64-simulator"   "ios"     "arm64-simulator" libwgpu_native.a no
copy-to-target "ios-x86_64-simulator"    "ios"     "amd64"           libwgpu_native.a
copy-to-target "windows-x86_64-gnu"      "windows" "amd64"           libwgpu_native.a
copy-to-target "windows-aarch64-gnullvm" "windows" "arm64"           libwgpu_native.a no
copy-to-target "linux-aarch64"           "linux"   "arm64"           libwgpu_native.a
copy-to-target "linux-x86_64"            "linux"   "amd64"           libwgpu_native.a
copy-to-target "android-aarch64"         "android" "arm64"           libwgpu_native.a
copy-to-target "android-x86_64"          "android" "amd64"           libwgpu_native.a
copy-to-target "android-armv7"           "android" "arm"             libwgpu_native.a
copy-to-target "android-i686"            "android" "386"             libwgpu_native.a

# the workflow's artifact carries no headers; they are the same for every
# slice of a release.
copy-headers windows-x86_64-gnu wgpu/lib/windows/arm64

write-notices

write-gitattributes

push-libs-repository

for SYSTEM in $SYSTEMS ; do
  env GOPROXY=direct go get github.com/belluikit/webgpu/libs-$SYSTEM@"$(git -C libs/repo rev-parse libs-"$SYSTEM")"
done

# re-generate enums & bindings
go generate ./wgpu

git add go.mod go.sum wgpu/
git commit -m "chore: update wgpu to $VERSION"
