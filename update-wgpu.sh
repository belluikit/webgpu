#!/usr/bin/env bash

set -ex -o pipefail

# gets the prebuilt release files from wgpu-native and installs
# the header files to wgpu/lib.
# the library files are pushed to branches according to the arch.
#
# belluikit fork: two slices on the libs-* branches are NOT produced by this
# script and must be refreshed by hand on a version bump: the iOS arm64
# Simulator slice (libs-ios/arm64-simulator, skipped below) and the GNU-ABI
# windows/arm64 archive (libs-windows/arm64/libwgpu_native.a, built by
# .github/workflows/build-wgpu-windows-arm64.yml). The windows-aarch64 line
# below still copies upstream's MSVC wgpu_native.lib, which the script would
# then push next to the GNU archive, so do not run it unmodified for a bump.
# See NOTICE.

VERSION="v29.0.1.1"

ZIPS="releases-$VERSION"

SYSTEMS="android darwin ios linux windows"

# download files and extract zip files.
# the resulting files are in $ZIPS/*/
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
          if [[ "$ZIP" = *-x86_64-msvc-* || "$ZIP" = *-aarch64-simulator-* ]] ; then
            continue
          fi

          ARCH=$(cut -d- -f2-3 <<< "$ZIP")
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

# create the libs repository and fetch latest branches
function fetch-libs-repository {
  mkdir -p libs

  pushd libs
    if ! [ -d repo/.git ] ; then
      mkdir repo
      pushd repo
        git init .
        git remote add origin https://github.com/belluikit/webgpu
      popd
    fi

    for SYSTEM in $SYSTEMS ; do
      git -C repo fetch origin libs-"$SYSTEM"
      git -C repo switch libs-"$SYSTEM"
    done
  popd
}

# push all library branches to the git repository
function push-libs-repository {
  for SYSTEM in $SYSTEMS ; do
    git -C libs/repo push origin libs-"$SYSTEM"
  done
}

# copy files into target directories
function copy-to-target() {
  local ZIP="$1"
  local TARGET="$2"
  local ARCH="$3"
  local LIB="$4"

  # copy headers
  mkdir -p wgpu/lib/"$TARGET"/"$ARCH"
  cp $ZIPS/zips/"$ZIP"/include/webgpu/*.h wgpu/lib/"$TARGET"/"$ARCH"
  git add "wgpu/lib/$TARGET/$ARCH"


  # switch to the correct library branch
  git -C libs/repo switch "libs-$TARGET"

  # copy lib to branch
  mkdir -p libs/repo/libs-"$TARGET"/"$ARCH"
  cp $ZIPS/zips/"$ZIP"/lib/"$LIB" libs/repo/libs-"$TARGET"/"$ARCH"
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
    windows) echo x86_64-pc-windows-gnu,aarch64-pc-windows-gnullvm,i686-pc-windows-msvc ;;
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
  cat > wgpu/lib/.gitattributes <<EOF
# See
# https://github.com/github/linguist/blob/249bbd1c2ffc631ca2ec628da26be5800eec3d48/docs/overrides.md#vendored-code

webgpu.h linguist-vendored
wgpu.h linguist-vendored
EOF
}

download-and-extract

fetch-libs-repository

copy-to-target "macos-aarch64"    "darwin"    "arm64"   libwgpu_native.a
copy-to-target "macos-x86_64"     "darwin"    "amd64"   libwgpu_native.a
copy-to-target "ios-aarch64"      "ios"       "arm64"   libwgpu_native.a
copy-to-target "ios-x86_64"       "ios"       "amd64"   libwgpu_native.a
copy-to-target "windows-aarch64"  "windows"   "arm64"   wgpu_native.lib
copy-to-target "windows-x86_64"   "windows"   "amd64"   libwgpu_native.a
copy-to-target "windows-i686"     "windows"   "386"     wgpu_native.lib
copy-to-target "linux-aarch64"    "linux"     "arm64"   libwgpu_native.a
copy-to-target "linux-x86_64"     "linux"     "amd64"   libwgpu_native.a
copy-to-target "android-aarch64"  "android"   "arm64"   libwgpu_native.a
copy-to-target "android-x86_64"   "android"   "amd64"   libwgpu_native.a
copy-to-target "android-armv7"    "android"   "arm"     libwgpu_native.a
copy-to-target "android-i686"     "android"   "386"     libwgpu_native.a

write-notices

write-gitattributes

push-libs-repository

for SYSTEM in $SYSTEMS ; do
  env GOPROXY=direct go get -u github.com/belluikit/webgpu/libs-$SYSTEM@libs-$SYSTEM
done

# re-generate enums & bindings
go generate ./wgpu

git add go.mod go.sum wgpu/lib/
git commit -m "chore: update wgpu to $VERSION"
