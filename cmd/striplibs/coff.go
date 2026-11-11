package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"strings"
)

// stripCOFFArchive strips each COFF member of ar on its own: bitcode
// sections go through llvm-objcopy, refused directives are dropped in place.
// Import members and the archive tables are kept as they are.
func stripCOFFArchive(objcopy string, ar *archive, st *stats) ([]byte, error) {
	for _, m := range ar.members {
		if m.special() || isImportMember(m.data) || objectFormat(m.data) != "coff" {
			continue
		}
		if empty, err := emptyCOFF(m.data); err != nil {
			return nil, fmt.Errorf("member %q: %v", m.name, err)
		} else if empty {
			// rustc leaves a header-only object for each .dwo; Go refuses
			// an object with no sections, and this one adds nothing.
			m.drop = true
			st.dropped++
			continue
		}
		f, err := pe.NewFile(bytes.NewReader(m.data))
		if err != nil {
			return nil, fmt.Errorf("member %q: %v", m.name, err)
		}
		var remove []string
		for _, s := range f.Sections {
			if isLTOSection(s.Name) {
				remove = append(remove, "--remove-section="+s.Name)
			}
		}
		if len(remove) > 0 {
			m.data, err = runObjcopy(objcopy, m.data, remove...)
			if err != nil {
				return nil, fmt.Errorf("member %q: %v", m.name, err)
			}
			st.objcopied++
		}
		n, err := filterDrectve(m.data)
		if err != nil {
			return nil, fmt.Errorf("member %q: %v", m.name, err)
		}
		st.directive += n
	}
	return ar.write()
}

// emptyCOFF reports whether obj is a COFF object with no sections. One with
// no sections but some symbols is an error: dropping it could lose them.
func emptyCOFF(obj []byte) (bool, error) {
	if len(obj) < 20 {
		return false, fmt.Errorf("truncated COFF header")
	}
	if binary.LittleEndian.Uint16(obj[2:4]) != 0 { // NumberOfSections
		return false, nil
	}
	if n := binary.LittleEndian.Uint32(obj[12:16]); n != 0 { // NumberOfSymbols
		return false, fmt.Errorf("COFF object with no sections has %d symbols", n)
	}
	return true, nil
}

// isLTOSection matches the section names Go's cgo check refuses
// (isDisallowedLTOSection in cmd/go/internal/work/security.go, go1.27.4).
func isLTOSection(name string) bool {
	return strings.HasPrefix(name, ".gnu.lto_") ||
		strings.HasPrefix(name, ".gnu.debuglto_") ||
		strings.HasPrefix(name, ".llvm.lto") ||
		name == ".llvmbc" ||
		name == ".llvmcmd"
}

// filterDrectve drops, in place, the -exclude-symbols: directives in the
// object's .drectve section that Go refuses, and pads the section with
// spaces to its old size, so nothing else in the object moves. It returns
// how many directives it dropped. Any other directive is kept.
func filterDrectve(obj []byte) (int, error) {
	f, err := pe.NewFile(bytes.NewReader(obj))
	if err != nil {
		return 0, err
	}
	for _, s := range f.Sections {
		if s.Name != ".drectve" || s.Size == 0 {
			continue
		}
		end := int64(s.Offset) + int64(s.Size)
		if end > int64(len(obj)) {
			return 0, fmt.Errorf(".drectve runs past the object's end")
		}
		sec := obj[s.Offset:end]
		var kept [][]byte
		dropped := 0
		for _, tok := range drectveTokens(sec) {
			if refusedExcludeSymbols(tok) {
				dropped++
				continue
			}
			kept = append(kept, tok)
		}
		if dropped == 0 {
			return 0, nil
		}
		joined := bytes.Join(kept, []byte(" "))
		if len(joined) > 0 {
			joined = append([]byte(" "), joined...) // directives conventionally lead with a space
		}
		copy(sec, joined)
		for i := len(joined); i < len(sec); i++ {
			sec[i] = ' '
		}
		return dropped, nil
	}
	return 0, nil
}

// drectveSpace separates directives, as in Go's tokenizeDrectve.
const drectveSpace = " \t\r\n\v\f\x00"

// drectveTokens splits a .drectve section into its directives as they are
// written, quotes included. A leading UTF-8 byte order mark is dropped.
func drectveTokens(sec []byte) [][]byte {
	s := bytes.TrimPrefix(sec, []byte("\xef\xbb\xbf"))
	var toks [][]byte
	for {
		s = bytes.TrimLeft(s, drectveSpace)
		if len(s) == 0 {
			return toks
		}
		i, inQuote := 0, false
		for ; i < len(s); i++ {
			if s[i] == '"' {
				inQuote = !inQuote
				continue
			}
			if !inQuote && bytes.IndexByte([]byte(drectveSpace), s[i]) >= 0 {
				break
			}
		}
		toks = append(toks, s[:i])
		s = s[i:]
	}
}

// refusedExcludeSymbols reports whether tok is an -exclude-symbols:
// directive whose value Go refuses: empty, or holding a path separator or
// "..". This mirrors the EXCLUDE-SYMBOLS case of Go's isAllowedDrectve.
func refusedExcludeSymbols(tok []byte) bool {
	t := strings.ReplaceAll(string(tok), `"`, "")
	norm := strings.ToUpper(t)
	if !strings.HasPrefix(norm, "-EXCLUDE-SYMBOLS:") && !strings.HasPrefix(norm, "/EXCLUDE-SYMBOLS:") {
		return false
	}
	val := t[len("-exclude-symbols:"):]
	return val == "" || strings.ContainsAny(val, `/\`) || strings.Contains(val, "..")
}
