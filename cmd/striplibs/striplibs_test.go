package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

// arMember is one member to lay out with buildArchive.
type arMember struct {
	name string // the header's name field
	data []byte
}

func buildArchive(members []arMember) []byte {
	var b bytes.Buffer
	b.WriteString(arMagic)
	for _, m := range members {
		fmt.Fprintf(&b, "%-16s%-12s%-6s%-6s%-8s%-10d`\n", m.name, "0", "0", "0", "644", len(m.data))
		b.Write(m.data)
		if len(m.data)%2 == 1 {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

// coffObject builds a COFF object for machine amd64 with one section per
// name, no symbols.
func coffObject(sections ...[2]string) []byte {
	var hdr, tab, data bytes.Buffer
	off := 20 + 40*len(sections)
	binary.Write(&hdr, binary.LittleEndian, struct {
		Machine, NumberOfSections                         uint16
		TimeDateStamp, PointerToSymbolTable, NumberOfSyms uint32
		SizeOfOptionalHeader, Characteristics             uint16
	}{0x8664, uint16(len(sections)), 0, 0, 0, 0, 0})
	for _, s := range sections {
		var name [8]byte
		copy(name[:], s[0])
		tab.Write(name[:])
		binary.Write(&tab, binary.LittleEndian, [8]uint32{
			0, 0, uint32(len(s[1])), uint32(off + data.Len()), 0, 0, 0, 0x100800, // LNK_INFO|LNK_REMOVE|ALIGN_1BYTES
		})
		data.WriteString(s[1])
	}
	return append(append(hdr.Bytes(), tab.Bytes()...), data.Bytes()...)
}

// memberAt returns the name of the member whose header starts at off.
func memberAt(t *testing.T, ar *archive, off int64) string {
	t.Helper()
	for _, m := range ar.members {
		if m.off == off {
			return m.name
		}
	}
	t.Fatalf("no member header at %d", off)
	return ""
}

func TestWriteRemapsSymbolTables(t *testing.T) {
	// Two symbol tables, as a Microsoft-style .lib has: the GNU one maps
	// symbols a and c to members A and C; the Microsoft one lists A and C.
	names := "a\x00c\x00"
	be := func(v ...uint32) []byte {
		b := make([]byte, 4*len(v))
		for i, x := range v {
			binary.BigEndian.PutUint32(b[4*i:], x)
		}
		return b
	}
	le := func(v ...uint32) []byte {
		b := make([]byte, 4*len(v))
		for i, x := range v {
			binary.LittleEndian.PutUint32(b[4*i:], x)
		}
		return b
	}
	longNames := "a-member-with-a-long-name.o/\n"
	lay := func(gnu, ms []byte) []byte {
		return buildArchive([]arMember{
			{"/", gnu},
			{"/", ms},
			{"//", []byte(longNames)},
			{"/0", []byte("AAAAA")}, // odd size: padded
			{"b.o/", coffObject()},  // empty: to be dropped
			{"c.o/", []byte("CCCCCCC")},
		})
	}
	tables := func(offA, offC uint32) ([]byte, []byte) {
		return append(be(2, offA, offC), names...),
			append(append(le(2, offA, offC, 2), 1, 0, 2, 0), names...)
	}
	// Lay out once with placeholder offsets, of the same size, to learn
	// where the members land.
	first, err := parseArchive(lay(tables(0, 0)))
	if err != nil {
		t.Fatal(err)
	}
	in := lay(tables(uint32(first.members[3].off), uint32(first.members[5].off)))

	ar, err := parseArchive(in)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := ar.write(); err != nil || !bytes.Equal(out, in) {
		t.Fatalf("unchanged archive does not round-trip: err=%v", err)
	}

	ar.members[3].data = []byte("AAAAAAAAAA") // grows, and loses its pad byte
	empty, err := emptyCOFF(ar.members[4].data)
	if err != nil || !empty {
		t.Fatalf("emptyCOFF = %v, %v; want true", empty, err)
	}
	ar.members[4].drop = true
	out, err := ar.write()
	if err != nil {
		t.Fatal(err)
	}

	got, err := parseArchive(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.members) != 5 {
		t.Fatalf("got %d members, want 5", len(got.members))
	}
	gnu, ms := got.members[0].data, got.members[1].data
	want := []string{"/0", "c.o/"}
	for i, w := range want {
		if n := memberAt(t, got, int64(binary.BigEndian.Uint32(gnu[4+4*i:]))); n != w {
			t.Errorf("GNU symbol table entry %d points at %q, want %q", i, n, w)
		}
		if n := memberAt(t, got, int64(binary.LittleEndian.Uint32(ms[4+4*i:]))); n != w {
			t.Errorf("Microsoft symbol table entry %d points at %q, want %q", i, n, w)
		}
	}
	if string(got.members[3].data) != "AAAAAAAAAA" || string(got.members[4].data) != "CCCCCCC" {
		t.Errorf("member contents changed: %q, %q", got.members[3].data, got.members[4].data)
	}
}

func TestWriteRefusesDanglingSymbol(t *testing.T) {
	in := buildArchive([]arMember{
		{"/", []byte("\x00\x00\x00\x01\x00\x00\x00\x09a\x00")}, // offset 9 is no member
		{"a.o/", []byte("AA")},
	})
	ar, err := parseArchive(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ar.write(); err == nil || !strings.Contains(err.Error(), "no member") {
		t.Fatalf("write = %v, want a dangling-entry error", err)
	}
}

func TestBSDLongNames(t *testing.T) {
	obj := []byte("\xcf\xfa\xed\xfe rest of a Mach-O object")
	in := buildArchive([]arMember{
		{"#1/12", []byte("__.SYMDEF\x00\x00\x00")},
		{"#1/8", append([]byte("x.o\x00\x00\x00\x00\x00"), obj...)},
	})
	ar, err := parseArchive(in)
	if err != nil {
		t.Fatal(err)
	}
	if !ar.members[0].special() {
		t.Errorf("%q is not taken for a symbol table", ar.members[0].name)
	}
	if ar.members[1].name != "x.o" || !bytes.Equal(ar.members[1].object(), obj) {
		t.Errorf("member = %q, %q", ar.members[1].name, ar.members[1].object())
	}
	if f := ar.format(); f != "macho" {
		t.Errorf("format = %q, want macho", f)
	}
}

func TestFilterDrectve(t *testing.T) {
	directives := ` -exclude-symbols:keep_me -exclude-symbols:_ZN4core3fmt..Debug "-exclude-symbols:q..uoted" /DEFAULTLIB:kernel32 -exclude-symbols:a/b`
	obj := coffObject([2]string{".text", "\xc3"}, [2]string{".drectve", directives})
	if err := verifyObject(obj); err == nil {
		t.Fatal("verify accepted refused directives")
	}

	before := len(obj)
	n, err := filterDrectve(obj)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("dropped %d directives, want 3", n)
	}
	if len(obj) != before {
		t.Errorf("object size changed: %d -> %d", before, len(obj))
	}
	sec := obj[len(obj)-len(directives):]
	if got := strings.TrimRight(string(sec), " "); got != " -exclude-symbols:keep_me /DEFAULTLIB:kernel32" {
		t.Errorf(".drectve = %q", got)
	}
	if err := verifyObject(obj); err != nil {
		t.Errorf("verify after filtering: %v", err)
	}
}

func TestEmptyCOFFWithSymbols(t *testing.T) {
	obj := coffObject()
	binary.LittleEndian.PutUint32(obj[12:16], 1) // NumberOfSymbols
	if _, err := emptyCOFF(obj); err == nil {
		t.Fatal("an object with no sections but a symbol was taken for empty")
	}
}

func TestVerifyRefusesLTOSection(t *testing.T) {
	obj := coffObject([2]string{".text", "\xc3"}, [2]string{".llvmbc", "BC\xc0\xde"})
	if err := verifyObject(obj); err == nil || !strings.Contains(err.Error(), ".llvmbc") {
		t.Fatalf("verify = %v, want .llvmbc refused", err)
	}
}
