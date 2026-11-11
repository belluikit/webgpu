package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	arMagic  = "!<arch>\n"
	arHdrLen = 60
)

// member is one entry of a Unix ar archive.
type member struct {
	hdr  [arHdrLen]byte
	name string // the member's name; a BSD long name ("#1/N") resolved
	data []byte // the member's contents, without the trailing pad byte
	off  int64  // offset of the header in the archive it was read from
	drop bool   // leave the member out when writing the archive
}

// special reports whether m is an archive table (symbol table, long names)
// rather than an object.
func (m *member) special() bool {
	switch m.name {
	case "/", "//", "/SYM64/", "/<ECSYMBOLS>/", "/<HYBRIDMAP>/",
		"__.SYMDEF", "__.SYMDEF SORTED", "__.SYMDEF_64", "__.SYMDEF_64 SORTED":
		return true
	}
	return false
}

// object returns the member's object bytes: for a BSD long name ("#1/N")
// the name is stored at the start of the data and is skipped.
func (m *member) object() []byte {
	if n, ok := strings.CutPrefix(strings.TrimRight(string(m.hdr[0:16]), " "), "#1/"); ok {
		if l, err := strconv.Atoi(n); err == nil && l <= len(m.data) {
			return m.data[l:]
		}
	}
	return m.data
}

type archive struct {
	members []*member
}

func parseArchive(data []byte) (*archive, error) {
	if !bytes.HasPrefix(data, []byte(arMagic)) {
		return nil, fmt.Errorf("not an ar archive")
	}
	ar := &archive{}
	off := int64(len(arMagic))
	for off < int64(len(data)) {
		if off+arHdrLen > int64(len(data)) {
			return nil, fmt.Errorf("truncated member header at %d", off)
		}
		m := &member{off: off}
		copy(m.hdr[:], data[off:off+arHdrLen])
		if m.hdr[58] != '`' || m.hdr[59] != '\n' {
			return nil, fmt.Errorf("bad member header at %d", off)
		}
		m.name = strings.TrimRight(string(m.hdr[0:16]), " ")
		size, err := strconv.ParseInt(strings.TrimSpace(string(m.hdr[48:58])), 10, 64)
		start := off + arHdrLen
		if err != nil || size < 0 || start+size > int64(len(data)) {
			return nil, fmt.Errorf("bad member size at %d", off)
		}
		m.data = data[start : start+size]
		if n, ok := strings.CutPrefix(m.name, "#1/"); ok {
			l, err := strconv.Atoi(n)
			if err != nil || l < 0 || int64(l) > size {
				return nil, fmt.Errorf("bad BSD long name at %d", off)
			}
			m.name = string(bytes.TrimRight(m.data[:l], "\x00"))
		}
		ar.members = append(ar.members, m)
		off = start + size + size%2
	}
	return ar, nil
}

// objectFormat names the object format of b: elf, macho, coff, or "" when
// b is none of them. A short import member counts as coff.
func objectFormat(b []byte) string {
	switch {
	case len(b) < 4:
		return ""
	case bytes.HasPrefix(b, []byte("\x7fELF")):
		return "elf"
	case binary.LittleEndian.Uint32(b) == 0xfeedfacf || binary.LittleEndian.Uint32(b) == 0xfeedface:
		return "macho"
	case isImportMember(b):
		return "coff"
	}
	switch binary.LittleEndian.Uint16(b) {
	case 0x14c, 0x8664, 0xaa64, 0x1c4: // i386, amd64, arm64, armnt
		return "coff"
	}
	return ""
}

// isImportMember reports whether b is a COFF short import member
// (IMPORT_OBJECT_HEADER: Sig1 0, Sig2 0xffff).
func isImportMember(b []byte) bool {
	return len(b) >= 4 && binary.LittleEndian.Uint16(b[0:2]) == 0 && binary.LittleEndian.Uint16(b[2:4]) == 0xffff
}

// format is the single object format of the archive's members.
func (ar *archive) format() string {
	found := ""
	for _, m := range ar.members {
		if m.special() {
			continue
		}
		f := objectFormat(m.object())
		if f == "" {
			continue
		}
		if found != "" && f != found {
			return "mixed"
		}
		found = f
	}
	return found
}

// write lays the members out as a GNU archive, and rewrites the member
// offsets the symbol tables hold so they point at the members' new
// positions. Only the size field of each header changes.
func (ar *archive) write() ([]byte, error) {
	moved := make(map[int64]int64, len(ar.members))
	off := int64(len(arMagic))
	for _, m := range ar.members {
		if m.drop {
			continue
		}
		moved[m.off] = off
		size := int64(len(m.data))
		off += arHdrLen + size + size%2
	}

	firstSlash := true
	var out bytes.Buffer
	out.WriteString(arMagic)
	for _, m := range ar.members {
		if m.drop {
			continue
		}
		data := m.data
		var err error
		switch m.name {
		case "/":
			// The first "/" is the GNU symbol table (big-endian), the second
			// the Microsoft one (little-endian); both map symbols to offsets.
			if firstSlash {
				data, err = remapOffsets(data, binary.BigEndian, 4, moved)
			} else {
				data, err = remapOffsets(data, binary.LittleEndian, 4, moved)
			}
			firstSlash = false
		case "/SYM64/":
			data, err = remapOffsets(data, binary.BigEndian, 8, moved)
		case "__.SYMDEF", "__.SYMDEF SORTED", "__.SYMDEF_64", "__.SYMDEF_64 SORTED":
			err = fmt.Errorf("BSD symbol table %q: rewriting BSD archives is not supported", m.name)
		}
		if err != nil {
			return nil, fmt.Errorf("member %q: %w", m.name, err)
		}

		hdr := m.hdr
		size := strconv.Itoa(len(data))
		if len(size) > 10 {
			return nil, fmt.Errorf("member %q too large", m.name)
		}
		copy(hdr[48:58], size+strings.Repeat(" ", 10-len(size)))
		out.Write(hdr[:])
		out.Write(data)
		if len(data)%2 == 1 {
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

// remapOffsets rewrites a symbol table that starts with a count followed by
// that many member offsets, each width bytes in byte order bo.
func remapOffsets(table []byte, bo binary.ByteOrder, width int, moved map[int64]int64) ([]byte, error) {
	get := func(b []byte) int64 {
		if width == 8 {
			return int64(bo.Uint64(b))
		}
		return int64(bo.Uint32(b))
	}
	put := func(b []byte, v int64) {
		if width == 8 {
			bo.PutUint64(b, uint64(v))
		} else {
			bo.PutUint32(b, uint32(v))
		}
	}
	if len(table) < width {
		return nil, fmt.Errorf("truncated symbol table")
	}
	n := get(table)
	if n < 0 || int64(width)*(n+1) > int64(len(table)) {
		return nil, fmt.Errorf("symbol table count %d exceeds its size", n)
	}
	out := bytes.Clone(table)
	for i := int64(1); i <= n; i++ {
		b := out[i*int64(width) : (i+1)*int64(width)]
		to, ok := moved[get(b)]
		if !ok {
			return nil, fmt.Errorf("symbol table entry %d points at %d, which is no member", i-1, get(b))
		}
		put(b, to)
	}
	return out, nil
}
