// Package dep5 extracts license texts from Debian machine-readable
// copyright files (DEP-5).
//
// Texts are returned verbatim, as the lines appear in the file, so their
// hash identifies exactly what was read. Copyright holders are never part
// of an extracted text; they belong to notices, not terms.
package dep5

import (
	"regexp"
	"strings"
)

// Field is one field of a paragraph with its raw lines. Lines[0] holds the
// "Name: value" line; the rest are continuation lines.
type Field struct {
	Name  string
	Lines []string
}

// Value returns the first-line value with surrounding space trimmed.
func (f Field) Value() string {
	_, v, _ := strings.Cut(f.Lines[0], ":")
	return strings.TrimSpace(v)
}

// HasBody reports whether the field has non-empty continuation lines.
func (f Field) HasBody() bool {
	for _, l := range f.Lines[1:] {
		if t := strings.TrimSpace(l); t != "" && t != "." {
			return true
		}
	}
	return false
}

// Raw returns the field exactly as written, newline terminated.
func (f Field) Raw() string { return strings.Join(f.Lines, "\n") + "\n" }

type Paragraph struct{ Fields []Field }

// Get returns the named field, matched case-insensitively.
func (p Paragraph) Get(name string) (Field, bool) {
	for _, f := range p.Fields {
		if strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return Field{}, false
}

// Parse splits data into paragraphs. Lines that are neither fields nor
// continuations are ignored.
func Parse(data []byte) []Paragraph {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	var paras []Paragraph
	var cur Paragraph
	flush := func() {
		if len(cur.Fields) > 0 {
			paras = append(paras, cur)
		}
		cur = Paragraph{}
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case line[0] == ' ' || line[0] == '\t':
			if n := len(cur.Fields); n > 0 {
				cur.Fields[n-1].Lines = append(cur.Fields[n-1].Lines, line)
			}
		case line[0] == '#':
		default:
			name, _, ok := strings.Cut(line, ":")
			if !ok || strings.ContainsAny(name, " \t") {
				continue
			}
			cur.Fields = append(cur.Fields, Field{Name: name, Lines: []string{line}})
		}
	}
	flush()
	return paras
}

// IsMachineReadable reports whether paras look like a DEP-5 file.
func IsMachineReadable(paras []Paragraph) bool {
	if len(paras) == 0 {
		return false
	}
	_, ok := paras[0].Get("Format")
	return ok
}

var splitter = regexp.MustCompile(`(?i)\s*(?:,\s*and\s+|,\s*or\s+|\s+and/or\s+|\s+and\s+|\s+or\s+|,\s*)`)

// Names splits a License short-name expression into its license names.
// "GPL-2+ with Autoconf exception" stays one name.
func Names(label string) []string {
	var out []string
	for _, n := range splitter.Split(strings.TrimSpace(label), -1) {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func sameName(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

// LicenseText returns the verbatim license text a Files paragraph refers
// to with label. selector, when non-empty, picks the Files paragraph whose
// pattern list contains it, so an inline license body there wins.
// Otherwise each name in label is resolved to a stand-alone License
// paragraph; texts are concatenated in label order. missing lists names
// with no text; the result is only complete when missing is empty.
func LicenseText(paras []Paragraph, label, selector string) (text string, missing []string) {
	// An inline body on the matching Files paragraph is the most specific.
	for _, p := range paras {
		files, ok := p.Get("Files")
		if !ok {
			continue
		}
		lic, ok := p.Get("License")
		if !ok || !sameName(lic.Value(), label) || !lic.HasBody() {
			continue
		}
		if selector == "" || containsPattern(files, selector) {
			return lic.Raw(), nil
		}
	}

	// standalone looks for the text of name in stand-alone License
	// paragraphs, then the header paragraph, then inline bodies on other
	// Files paragraphs. Inline bodies are used only when they all agree,
	// since different bodies under one name are different terms.
	standalone := func(name string) (Field, bool) {
		var header, inline []Field
		for _, p := range paras {
			lic, ok := p.Get("License")
			if !ok || !sameName(lic.Value(), name) || !lic.HasBody() {
				continue
			}
			_, isFiles := p.Get("Files")
			_, isHeader := p.Get("Format")
			switch {
			case isFiles:
				inline = append(inline, lic)
			case isHeader:
				header = append(header, lic)
			default:
				return lic, true
			}
		}
		if len(header) > 0 {
			return header[0], true
		}
		for _, f := range inline[min(1, len(inline)):] {
			if f.Raw() != inline[0].Raw() {
				return Field{}, false
			}
		}
		if len(inline) > 0 {
			return inline[0], true
		}
		return Field{}, false
	}

	if f, ok := standalone(label); ok {
		return f.Raw(), nil
	}
	var parts []string
	for _, name := range Names(label) {
		f, ok := standalone(name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		parts = append(parts, f.Raw())
	}
	if len(missing) > 0 {
		return "", missing
	}
	return strings.Join(parts, "\n"), nil
}

func containsPattern(files Field, selector string) bool {
	for i, l := range files.Lines {
		if i == 0 {
			l = files.Value()
		}
		for _, pat := range strings.Fields(l) {
			if pat == selector {
				return true
			}
		}
	}
	return false
}
