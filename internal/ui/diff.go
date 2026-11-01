package ui

import "strings"

// diffOp is one line of a line diff.
type diffOp struct {
	op   byte // ' ', '+', '-'
	text string
}

// lineDiff diffs two texts by lines (LCS; unit files are small).
func lineDiff(a, b string) []diffOp {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	x, y := split(a), split(b)
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []diffOp
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && x[i] == y[j]:
			out = append(out, diffOp{' ', x[i]})
			i++
			j++
		case j < m && (i >= n || lcs[i][j+1] >= lcs[i+1][j]):
			out = append(out, diffOp{'+', y[j]})
			j++
		default:
			out = append(out, diffOp{'-', x[i]})
			i++
		}
	}
	return out
}

// diffStat counts added and removed lines.
func diffStat(ops []diffOp) (add, del int) {
	for _, o := range ops {
		switch o.op {
		case '+':
			add++
		case '-':
			del++
		}
	}
	return
}
