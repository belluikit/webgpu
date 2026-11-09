// Command notices writes the third-party notices of a wgpu-native build: the
// Rust packages compiled into libwgpu_native for the given targets, each with
// its license and license text, and the Rust standard library's notices
// (belluikit/webgpu#17).
//
// The package list is cargo's own: `cargo tree -e normal,no-proc-macro` for
// each target, which is what `make package` (wgpu-native's release build,
// default features) compiles in. License texts are the files each package
// ships (LICENSE*, COPYING*, NOTICE*, UNLICENSE*). A package that ships none
// gets its repository's text when ./licenses/upstream pins one for that
// version, else the canonical text of the license its Cargo.toml declares,
// from ./licenses; a declared license with no canonical text here is an error, so
// a notices file never ships with a gap.
//
// The Rust standard library's notices come from the COPYRIGHT-library.html
// of each rustc that built a library in the set (read from the library:
// rustc records its version in the archive). A platform's slices can come
// from different rustc versions, so -std takes one file per version.
//
//	notices -src wgpu-native -targets aarch64-linux-android,... \
//	        -std 1.91.1=COPYRIGHT-library.html -o THIRD_PARTY_NOTICES.txt
package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed licenses/*.txt licenses/upstream/*.txt
var canonical embed.FS

type pkg struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	License      string   `json:"license"`
	LicenseFile  string   `json:"license_file"`
	ManifestPath string   `json:"manifest_path"`
	Repository   string   `json:"repository"`
	Authors      []string `json:"authors"`
}

type text struct {
	body  string
	users []string // "name version (file)"
}

func main() {
	src := flag.String("src", "", "wgpu-native checkout at the released tag")
	targets := flag.String("targets", "", "comma-separated Rust target triples the library is built for")
	std := flag.String("std", "", "comma-separated version=COPYRIGHT-library.html, one per rustc that built a library in the set")
	out := flag.String("o", "", "output file")
	flag.Parse()
	if *src == "" || *targets == "" || *std == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	var stds [][2]string
	for _, kv := range strings.Split(*std, ",") {
		v, path, ok := strings.Cut(kv, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "notices: -std entry %q is not version=path\n", kv)
			os.Exit(2)
		}
		stds = append(stds, [2]string{v, path})
	}
	if err := run(*src, strings.Split(*targets, ","), stds, *out); err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run(src string, targets []string, stds [][2]string, out string) error {
	tag, err := cmdOut(src, "git", "describe", "--tags", "--exact-match")
	if err != nil {
		return fmt.Errorf("%s is not at a release tag: %w", src, err)
	}
	tag = strings.TrimSpace(tag)

	built := map[string]bool{} // "name version"
	for _, t := range targets {
		tree, err := cmdOut(src, "cargo", "tree", "--locked", "--target", t, "-e", "normal,no-proc-macro", "--prefix", "none")
		if err != nil {
			return fmt.Errorf("cargo tree --target %s: %w", t, err)
		}
		for _, line := range strings.Split(tree, "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				built[f[0]+" "+strings.TrimPrefix(f[1], "v")] = true
			}
		}
	}

	meta, err := cmdOut(src, "cargo", "metadata", "--locked", "--format-version", "1")
	if err != nil {
		return fmt.Errorf("cargo metadata: %w", err)
	}
	var m struct{ Packages []pkg }
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		return err
	}
	var pkgs []pkg
	for _, p := range m.Packages {
		if built[p.Name+" "+p.Version] {
			pkgs = append(pkgs, p)
			delete(built, p.Name+" "+p.Version)
		}
	}
	if len(built) > 0 {
		return fmt.Errorf("cargo tree lists packages cargo metadata does not: %v", keys(built))
	}
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Name != pkgs[j].Name {
			return pkgs[i].Name < pkgs[j].Name
		}
		return pkgs[i].Version < pkgs[j].Version
	})

	texts := map[[32]byte]*text{}
	var order [][32]byte
	add := func(body, user string) {
		body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
		h := sha256.Sum256([]byte(body))
		t := texts[h]
		if t == nil {
			t = &text{body: body}
			texts[h] = t
			order = append(order, h)
		}
		t.users = append(t.users, user)
	}
	for _, p := range pkgs {
		files, err := licenseFiles(p)
		if err != nil {
			return err
		}
		id := p.Name + " " + p.Version
		if len(files) > 0 {
			for _, f := range files {
				b, err := os.ReadFile(f)
				if err != nil {
					return err
				}
				add(string(b), id+" ("+filepath.Base(f)+")")
			}
			continue
		}
		if b, err := canonical.ReadFile("licenses/upstream/" + p.Name + "-" + p.Version + ".txt"); err == nil {
			add(string(b), id+" (ships no license file; text from its repository, cmd/notices/licenses/upstream)")
			continue
		}
		body, name, err := canonicalText(p)
		if err != nil {
			return err
		}
		add(body, id+" (ships no license file; canonical "+name+" text)")
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "Third-party notices for wgpu-native %s\n", tag)
	fmt.Fprintf(&b, "Built for: %s\n\n", strings.Join(targets, ", "))
	fmt.Fprintf(&b, "Generated by belluikit/webgpu cmd/notices from wgpu-native's Cargo.lock\n")
	fmt.Fprintf(&b, "and each package's own files. Do not edit; update-wgpu.sh regenerates it.\n\n")
	fmt.Fprintf(&b, "wgpu-native is compiled from the %d Rust packages below and the Rust\n", len(pkgs))
	fmt.Fprintf(&b, "standard library of rustc %s.\n\n", stdVersions(stds))
	for _, p := range pkgs {
		fmt.Fprintf(&b, "  %s %s  %s  %s\n", p.Name, p.Version, p.License, p.Repository)
	}
	fmt.Fprintf(&b, "\n\nLICENSE TEXTS\n=============\n")
	for _, h := range order {
		t := texts[h]
		fmt.Fprintf(&b, "\n----------------------------------------------------------------------\n")
		fmt.Fprintf(&b, "Used by: %s\n\n%s\n", strings.Join(t.users, "; "), t.body)
	}
	for _, st := range stds {
		h, err := os.ReadFile(st[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "\n----------------------------------------------------------------------\n")
		fmt.Fprintf(&b, "The Rust standard library, rustc %s (its COPYRIGHT-library.html)\n\n%s\n", st[0], htmlText(string(h)))
	}
	return os.WriteFile(out, b.Bytes(), 0o644)
}

func stdVersions(stds [][2]string) string {
	var v []string
	for _, st := range stds {
		v = append(v, st[0])
	}
	return strings.Join(v, " and ")
}

var licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|unlicense)`)

func licenseFiles(p pkg) ([]string, error) {
	dir := filepath.Dir(p.ManifestPath)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range ents {
		if !e.IsDir() && licenseName.MatchString(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 && p.LicenseFile != "" {
		files = append(files, p.LicenseFile)
	}
	sort.Strings(files)
	return files, nil
}

// canonicalText picks, from a package's declared SPDX expression, a license
// this command has the canonical text of. An OR expression is the user's
// choice of one, so the first one available wins; an AND needs every part's
// text and a file the package does not ship, so it is an error.
func canonicalText(p pkg) (body, name string, err error) {
	expr := p.License
	if strings.Contains(expr, " AND ") || expr == "" {
		return "", "", fmt.Errorf("%s %s ships no license file and declares %q; add its text by hand", p.Name, p.Version, expr)
	}
	for _, id := range strings.FieldsFunc(expr, func(r rune) bool { return r == '/' || r == '(' || r == ')' || r == ' ' }) {
		if id == "OR" {
			continue
		}
		b, err := canonical.ReadFile("licenses/" + id + ".txt")
		if err != nil {
			continue
		}
		s := string(b)
		if strings.Contains(s, "<copyright holders>") {
			if len(p.Authors) == 0 {
				continue
			}
			s = strings.ReplaceAll(s, "<year> <copyright holders>", strings.Join(p.Authors, ", "))
		}
		return s, id, nil
	}
	return "", "", fmt.Errorf("%s %s ships no license file and declares %q, which has no canonical text in cmd/notices/licenses", p.Name, p.Version, expr)
}

var (
	tagRe   = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	blankRe = regexp.MustCompile(`\n[ \t]*\n([ \t]*\n)+`)
)

func htmlText(s string) string {
	s = strings.NewReplacer("<br>", "\n", "</p>", "\n\n", "</li>", "\n", "</h1>", "\n\n", "</h2>", "\n\n", "</h3>", "\n\n", "</pre>", "\n").Replace(s)
	s = html.UnescapeString(tagRe.ReplaceAllString(s, ""))
	return strings.TrimSpace(blankRe.ReplaceAllString(s, "\n\n"))
}

func cmdOut(dir, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	var stderr bytes.Buffer
	c.Stderr = &stderr
	o, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return string(o), nil
}

func keys(m map[string]bool) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
