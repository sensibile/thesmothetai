// Package textdiff produces line diffs of license texts for review.
package textdiff

import "strings"

type Op byte

const (
	Equal  Op = ' '
	Insert Op = '+'
	Delete Op = '-'
)

type Line struct {
	Op   Op
	Text string
}

// maxCells bounds the LCS table; larger inputs fall back to a full
// replacement diff.
const maxCells = 4_000_000

// Lines returns a line-level diff turning a into b.
func Lines(a, b string) []Line {
	as, bs := split(a), split(b)
	n, m := len(as), len(bs)
	if n*m > maxCells {
		out := make([]Line, 0, n+m)
		for _, l := range as {
			out = append(out, Line{Delete, l})
		}
		for _, l := range bs {
			out = append(out, Line{Insert, l})
		}
		return out
	}

	// lcs[i][j] is the LCS length of as[i:] and bs[j:].
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if as[i] == bs[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	out := make([]Line, 0, max(n, m))
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case as[i] == bs[j]:
			out = append(out, Line{Equal, as[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, Line{Delete, as[i]})
			i++
		default:
			out = append(out, Line{Insert, bs[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, Line{Delete, as[i]})
	}
	for ; j < m; j++ {
		out = append(out, Line{Insert, bs[j]})
	}
	return out
}

func split(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
