package main

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
)

// verify checks that a stripped archive holds none of what striplibs
// removes: no LTO section in any object, no __LLVM segment in a Mach-O
// object, and no refused -exclude-symbols: directive. It also requires
// every member to be an object Go can read, since Go refuses a library
// holding anything else. It is a check of striplibs' own work, not a copy of
// Go's whole check; building for the target is that.
func verify(data []byte) error {
	ar, err := parseArchive(data)
	if err != nil {
		return err
	}
	if ar.format() == "coff" {
		// Every symbol table entry must point at a member header, and
		// writing the archive back must reproduce it byte for byte.
		out, err := ar.write()
		if err != nil {
			return err
		}
		if !bytes.Equal(out, data) {
			return fmt.Errorf("archive does not round-trip")
		}
	}
	for _, m := range ar.members {
		if m.special() {
			continue
		}
		if err := verifyObject(m.object()); err != nil {
			return fmt.Errorf("member %q: %w", m.name, err)
		}
	}
	return nil
}

func verifyObject(b []byte) error {
	switch objectFormat(b) {
	case "elf":
		f, err := elf.NewFile(bytes.NewReader(b))
		if err != nil {
			return err
		}
		for _, s := range f.Sections {
			if isLTOSection(s.Name) {
				return fmt.Errorf("section %s", s.Name)
			}
		}
	case "macho":
		f, err := macho.NewFile(bytes.NewReader(b))
		if err != nil {
			return err
		}
		for _, l := range f.Loads {
			if s, ok := l.(*macho.Segment); ok && s.Name == "__LLVM" {
				return fmt.Errorf("segment __LLVM")
			}
		}
		for _, s := range f.Sections {
			if s.Seg == "__LLVM" || isLTOSection(s.Name) {
				return fmt.Errorf("section %s,%s", s.Seg, s.Name)
			}
		}
	case "coff":
		if isImportMember(b) {
			return nil
		}
		f, err := pe.NewFile(bytes.NewReader(b))
		if err != nil {
			return err
		}
		if len(f.Sections) == 0 {
			return fmt.Errorf("no sections")
		}
		for _, s := range f.Sections {
			if isLTOSection(s.Name) {
				return fmt.Errorf("section %s", s.Name)
			}
			if s.Name != ".drectve" {
				continue
			}
			d, err := s.Data()
			if err != nil {
				return err
			}
			for _, tok := range drectveTokens(d) {
				if refusedExcludeSymbols(tok) {
					return fmt.Errorf(".drectve directive %s", tok)
				}
			}
		}
	default:
		return fmt.Errorf("not an ELF, Mach-O or COFF object")
	}
	return nil
}
