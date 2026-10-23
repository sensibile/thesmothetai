package textdiff

import (
	"strings"
	"testing"
)

func render(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteByte(byte(l.Op))
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestLines(t *testing.T) {
	got := render(Lines("a\nb\nc\n", "a\nB\nc\nd\n"))
	want := " a\n-b\n+B\n c\n+d\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
