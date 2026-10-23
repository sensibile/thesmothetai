package evidence

import "testing"

func TestParsePURL(t *testing.T) {
	c, err := ParsePURL("pkg:npm/%40scope/name@1.2.3?arch=x64")
	if err != nil {
		t.Fatal(err)
	}
	if c.Package != "pkg:npm/%40scope/name" || c.Version != "1.2.3" {
		t.Fatalf("got %+v", c)
	}
	for _, bad := range []string{"npm/x@1", "pkg:npm/x", "pkg:npm/x@", "pkg:x@1"} {
		if _, err := ParsePURL(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}
