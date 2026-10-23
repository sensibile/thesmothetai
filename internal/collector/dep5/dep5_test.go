package dep5

import (
	"slices"
	"strings"
	"testing"
)

const sample = `Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: demo

Files: *
Copyright: 2020 Alice
License: GPL-2+ or Artistic

Files: lib/*
Copyright: 2021 Bob
License: Custom
 You may do anything with lib.
 .
 No warranty.

Files: doc/*
Copyright: 2022 Carol
License: GFDL-1.3+

License: GPL-2+
 This program is free software; you can redistribute it under the GPL,
 version 2 or (at your option) any later version.

License: Artistic
 The "Artistic License".
`

func TestLicenseText(t *testing.T) {
	paras := Parse([]byte(sample))
	if !IsMachineReadable(paras) {
		t.Fatal("expected DEP-5")
	}

	text, missing := LicenseText(paras, "GPL-2+ or Artistic", "*")
	if len(missing) != 0 {
		t.Fatalf("missing %v", missing)
	}
	if !strings.HasPrefix(text, "License: GPL-2+\n This program") || !strings.Contains(text, "License: Artistic\n") {
		t.Fatalf("got %q", text)
	}
	if strings.Contains(text, "Alice") {
		t.Fatal("copyright holders must not be part of the text")
	}

	text, _ = LicenseText(paras, "Custom", "lib/*")
	if text != "License: Custom\n You may do anything with lib.\n .\n No warranty.\n" {
		t.Fatalf("inline body: got %q", text)
	}

	if _, missing := LicenseText(paras, "GFDL-1.3+", "doc/*"); !slices.Equal(missing, []string{"GFDL-1.3+"}) {
		t.Fatalf("expected GFDL-1.3+ missing, got %v", missing)
	}

	// An inline body on another Files paragraph is reused when unambiguous.
	extra := sample + "\nFiles: tools/*\nCopyright: 2023 Dan\nLicense: Custom\n"
	text, _ = LicenseText(Parse([]byte(extra)), "Custom", "tools/*")
	if !strings.Contains(text, "You may do anything with lib.") {
		t.Fatalf("expected shared inline body, got %q", text)
	}

	// Conflicting inline bodies under one name are not guessed between.
	conflict := extra + "\nFiles: other/*\nCopyright: 2024 Eve\nLicense: Custom\n Different terms.\n"
	if _, missing := LicenseText(Parse([]byte(conflict)), "Custom", "tools/*"); len(missing) != 1 {
		t.Fatalf("expected ambiguity to stay unresolved, got %v", missing)
	}
}

func TestHeaderLicense(t *testing.T) {
	doc := "Format: x\nUpstream-Name: y\nLicense: GPL-2+\n Header terms.\n\nFiles: *\nCopyright: 2020 Z\nLicense: GPL-2+\n"
	text, missing := LicenseText(Parse([]byte(doc)), "GPL-2+", "*")
	if len(missing) != 0 || text != "License: GPL-2+\n Header terms.\n" {
		t.Fatalf("got %q %v", text, missing)
	}
}

func TestNames(t *testing.T) {
	got := Names("REGCOMP, and GPL-1+ or Artistic")
	if !slices.Equal(got, []string{"REGCOMP", "GPL-1+", "Artistic"}) {
		t.Fatalf("got %v", got)
	}
	if got := Names("GPL-2+ with Autoconf exception"); len(got) != 1 {
		t.Fatalf("exception clause must stay attached: %v", got)
	}
}
