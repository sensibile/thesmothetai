package fingerprint

import "testing"

func TestHashIsExact(t *testing.T) {
	a := Hash([]byte("MIT License\n"))
	b := Hash([]byte("MIT License \n"))
	if a == b {
		t.Fatal("whitespace change must change the exact hash")
	}
}

func TestNormalizedHashIgnoresFormatting(t *testing.T) {
	a := NormalizedHash([]byte("Permission is hereby\r\ngranted,  free"))
	b := NormalizedHash([]byte("Permission is hereby granted, free\n"))
	if a != b {
		t.Fatal("formatting-only differences should share a normalized hash")
	}
	if a == NormalizedHash([]byte("permission is hereby granted, free")) {
		t.Fatal("case changes must not be normalized away")
	}
}
