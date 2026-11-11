// Command striplibs removes from wgpu-native's prebuilt static libraries the
// sections that cgo refuses to link since Go 1.27.4 (#24).
//
// Go 1.27.4 checks every library a #cgo LDFLAGS names, and refuses it when an
// object inside carries LLVM bitcode (.llvmbc and .llvmcmd in ELF and COFF,
// the __LLVM segment in Mach-O) or a COFF .drectve directive it does not
// allow. Every libs-* slice trips the first: the Rust standard library ships
// precompiled with embedded bitcode, so a staticlib carries it whatever flags
// wgpu-native is built with. The Windows GNU slices also trip the second:
// rustc writes an -exclude-symbols: directive for each symbol a DLL must not
// re-export, and Go refuses any whose value contains "..", which Rust's
// legacy symbol mangling uses for "::".
//
// Neither is needed to link: the bitcode is only read for cross-language LTO,
// and -exclude-symbols: only matters when the library is linked into a DLL.
// So striplibs removes the bitcode sections, drops the refused
// -exclude-symbols: directives and keeps the rest, and then checks its own
// result.
//
// Usage:
//
//	go run ./cmd/striplibs [-objcopy llvm-objcopy] lib.a...
//
// Each library is rewritten in place. ELF and Mach-O archives go through
// llvm-objcopy whole. COFF archives are rewritten member by member, because
// llvm-objcopy cannot process an archive holding short import members (the
// windows-link crate puts some in), and extracting and re-archiving loses
// members that share a name.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	objcopy := flag.String("objcopy", "llvm-objcopy", "llvm-objcopy binary")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: striplibs [-objcopy path] lib.a...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	failed := false
	for _, path := range flag.Args() {
		st, err := strip(*objcopy, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "striplibs: %s: %v\n", path, err)
			failed = true
			continue
		}
		fmt.Printf("%s: %s\n", path, st)
	}
	if failed {
		os.Exit(1)
	}
}

// stats reports what strip changed in one library.
type stats struct {
	format               string
	before, after        int64
	objcopied, directive int // members rewritten by llvm-objcopy; directives dropped
	dropped              int // empty members left out
}

func (s stats) String() string {
	out := fmt.Sprintf("%s, %d -> %d bytes", s.format, s.before, s.after)
	if s.format == "coff" {
		out += fmt.Sprintf(", %d members stripped, %d directives dropped, %d empty members dropped",
			s.objcopied, s.directive, s.dropped)
	}
	return out
}

func strip(objcopy, path string) (stats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return stats{}, err
	}
	ar, err := parseArchive(data)
	if err != nil {
		return stats{}, err
	}
	st := stats{format: ar.format(), before: int64(len(data))}

	var out []byte
	switch st.format {
	case "coff":
		out, err = stripCOFFArchive(objcopy, ar, &st)
	case "elf":
		out, err = runObjcopy(objcopy, data, "--remove-section=.llvmbc", "--remove-section=.llvmcmd")
	case "macho":
		out, err = runObjcopy(objcopy, data, "--remove-section=__LLVM,__bitcode", "--remove-section=__LLVM,__cmdline")
	default:
		err = fmt.Errorf("no object members found")
	}
	if err != nil {
		return st, err
	}

	if err := verify(out); err != nil {
		return st, fmt.Errorf("after stripping: %w", err)
	}
	st.after = int64(len(out))
	if bytes.Equal(out, data) {
		return st, nil
	}
	return st, os.WriteFile(path, out, 0o644)
}

// runObjcopy runs llvm-objcopy over data (an object or an archive) and
// returns its output.
func runObjcopy(objcopy string, data []byte, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "striplibs")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		return nil, err
	}
	cmd := exec.Command(objcopy, append(args, in, out)...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s: %v: %s", objcopy, err, bytes.TrimSpace(msg))
	}
	return os.ReadFile(out)
}
