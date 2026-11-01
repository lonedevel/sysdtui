package catalog

import (
	"regexp"
	"strings"
)

// Para is one block of a directive's documentation: a paragraph to be
// wrapped, or a preformatted line (Pre) that keeps its layout.
type Para struct {
	Indent int    `json:"i,omitempty"`
	Text   string `json:"t"`
	Pre    bool   `json:"p,omitempty"`
}

// entry is a directive block found in a man page: the names in its tag
// and its body, still in troff.
type entry struct {
	names   []string
	heading string // the .SH heading it sits under
	body    []string
}

var (
	tagStart = regexp.MustCompile(`^\\fI[A-Z][A-Za-z0-9]*=`)
	tagName  = regexp.MustCompile(`(?:^|, )([A-Z][A-Za-z0-9]*)=`)
)

// tagNames returns the directive names on a tag line, or nil when the
// line is prose. Tags look like `\fIBefore=\fR, \fIAfter=\fR` or carry an
// argument: `\fICPUWeight=\fR\fI\fIweight\fR\fR`, `\fIDevicePolicy=auto|closed|strict\fR`.
func tagNames(line string) []string {
	if !tagStart.MatchString(line) {
		return nil
	}
	var names []string
	for _, m := range tagName.FindAllStringSubmatch(inline(line), -1) {
		names = append(names, m[1])
	}
	return names
}

// scanEntries finds the directive blocks in a page's troff source:
// .PP, one or more tag lines naming directives, then an .RS 4 … .RE body.
func scanEntries(src string) []entry {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []entry
	heading := ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, ".SH ") {
			heading = strings.ToUpper(strings.Trim(strings.TrimPrefix(l, ".SH "), `" `))
			continue
		}
		if l != ".PP" {
			continue
		}
		var names []string
		j := i + 1
		for ; j < len(lines); j++ {
			n := tagNames(lines[j])
			if n == nil {
				break
			}
			names = append(names, n...)
		}
		if len(names) == 0 || j >= len(lines) || !strings.HasPrefix(lines[j], ".RS") {
			continue
		}
		depth, k := 1, j+1
		for ; k < len(lines) && depth > 0; k++ {
			switch {
			case strings.HasPrefix(lines[k], ".RS"):
				depth++
			case strings.HasPrefix(lines[k], ".RE"):
				depth--
			}
		}
		out = append(out, entry{names: names, heading: heading, body: lines[j+1 : k-1]})
		i = k - 1
	}
	return out
}

var (
	escFont    = regexp.MustCompile(`\\f(\[[^\]]*\]|\(..|.)`)
	escFoot    = regexp.MustCompile(`\\s-2\\u.*?\\d\\s\+2`)
	escColor   = regexp.MustCompile(`\\m\[[^\]]*\]`)
	escMotion  = regexp.MustCompile(`\\h'-[^']*'`)
	escSpace   = regexp.MustCompile(`\\h'\+?[^']*'`)
	escSize    = regexp.MustCompile(`\\s[-+]?\d`)
	escComment = regexp.MustCompile(`\\".*$`)
)

var escReplacer = strings.NewReplacer(
	`\*(Aq`, "'", `\(aq`, "'", `\(em`, "—", `\(en`, "–", `\(bu`, "•",
	`\(lq`, "“", `\(rq`, "”", `\(dq`, `"`, `\(cq`, "'", `\(oq`, "'",
	`\(ti`, "~", `\(ha`, "^", `\(rs`, `\`, `\(at`, "@", `\(-> `, "→ ",
	`\(->`, "→", `\(<=`, "≤", `\(>=`, "≥", `\(mu`, "×",
	`\-`, "-", `\e`, `\`, `\ `, " ", `\~`, " ", `\%`, "", `\&`, "", `\:`, "",
	`\|`, "", `\^`, "", `\0`, " ", `\u`, "", `\d`, "",
)

// inline converts troff escapes in a line of text to plain text.
func inline(s string) string {
	s = escComment.ReplaceAllString(s, "")
	s = escFoot.ReplaceAllString(s, "")
	s = escColor.ReplaceAllString(s, "")
	s = escMotion.ReplaceAllString(s, "")
	s = escSpace.ReplaceAllString(s, " ")
	s = escFont.ReplaceAllString(s, "")
	s = escSize.ReplaceAllString(s, "")
	return escReplacer.Replace(s)
}

// render converts a directive body from troff to paragraphs. It handles
// the subset of man(7) that DocBook emits for systemd's pages.
func render(body []string) []Para {
	var (
		out     []Para
		cur     []string
		indent  int
		pre     bool
		joinNxt bool // the previous line ended in \c
		skip    int  // depth inside .el blocks, which hold the typeset variant
	)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, Para{Indent: indent, Text: strings.Join(cur, " ")})
			cur = nil
		}
	}
	for i := 0; i < len(body); i++ {
		l := body[i]
		if skip > 0 {
			if strings.HasSuffix(l, `\{\`) || strings.HasSuffix(l, `\{`) {
				skip++
			}
			if strings.HasPrefix(l, `.\}`) || strings.HasSuffix(l, `\}`) {
				skip--
			}
			continue
		}
		if strings.HasPrefix(l, ".el") {
			if strings.Contains(l, `\{`) {
				skip = 1
			}
			continue
		}
		if strings.HasPrefix(l, ".ie n ") || strings.HasPrefix(l, ".if n ") {
			rest := strings.TrimSpace(l[6:])
			if rest == `\{\` || rest == `\{` {
				continue // the body follows on the next lines
			}
			l = strings.TrimPrefix(rest, `\{`)
		}
		if strings.HasPrefix(l, ".ie t ") || strings.HasPrefix(l, ".if t ") || strings.HasPrefix(l, ".if ") {
			if strings.HasSuffix(l, `\{\`) {
				skip = 1
			}
			continue
		}
		if strings.HasPrefix(l, `.\}`) {
			continue
		}
		if strings.HasPrefix(l, ".TS") {
			flush()
			j := i + 1
			out = append(out, table(body, &j, indent)...)
			i = j
			continue
		}
		if pre {
			if strings.HasPrefix(l, ".fi") {
				pre = false
				continue
			}
			if strings.HasPrefix(l, ".") && !strings.HasPrefix(l, `.\`) {
				continue
			}
			out = append(out, Para{Indent: indent, Text: inline(l), Pre: true})
			continue
		}
		if strings.HasPrefix(l, ".") || strings.HasPrefix(l, "'") {
			req := strings.Fields(l)
			switch req[0] {
			case ".sp", ".PP", ".br", ".LP", ".P":
				flush()
			case ".RS":
				flush()
				indent += 4
			case ".RE":
				flush()
				if indent >= 4 {
					indent -= 4
				}
			case ".nf":
				flush()
				pre = true
			case ".IP":
				flush()
				if len(req) > 1 {
					cur = append(cur, inline(strings.Trim(req[1], `"`)))
				}
			case ".B", ".I", ".BR", ".IR":
				if len(req) > 1 {
					cur = append(cur, inline(strings.Trim(strings.Join(req[1:], " "), `"`)))
				}
			}
			continue
		}
		text := l
		cont := strings.HasSuffix(text, `\c`)
		text = inline(strings.TrimSuffix(text, `\c`))
		if joinNxt && len(cur) > 0 {
			cur[len(cur)-1] += text
		} else if cont {
			cur = append(cur, strings.TrimLeft(text, " "))
		} else if t := strings.TrimSpace(text); t != "" {
			cur = append(cur, t)
		}
		joinNxt = cont
	}
	flush()
	// A bullet comes out as its own word ("•" then the text): close it up.
	for i := range out {
		out[i].Text = strings.TrimSpace(out[i].Text)
	}
	return out
}

// table renders a tbl(1) table as preformatted rows, cells separated by
// " │ ". *i points just after .TS and is left on .TE.
func table(body []string, i *int, indent int) []Para {
	sep := "\t"
	inFormat := true
	var rows [][]string
	var row []string
	var cell []string
	inCell := false
	for ; *i < len(body); *i++ {
		l := body[*i]
		if strings.HasPrefix(l, ".TE") {
			break
		}
		if inFormat {
			if k := strings.Index(l, "tab("); k >= 0 && len(l) > k+5 {
				sep = string(l[k+4])
			}
			if strings.HasSuffix(l, ".") {
				inFormat = false
			}
			continue
		}
		if strings.HasPrefix(l, ".T&") {
			inFormat = true
			continue
		}
		if strings.HasPrefix(l, ".") {
			continue
		}
		if inCell {
			if strings.HasPrefix(l, "T}") {
				row = append(row, strings.Join(cell, " "))
				cell, inCell = nil, false
				rest := strings.TrimPrefix(strings.TrimPrefix(l, "T}"), sep)
				if rest == "" {
					rows = append(rows, row)
					row = nil
					continue
				}
				l = rest
			} else {
				cell = append(cell, inline(l))
				continue
			}
		}
		for _, c := range strings.Split(l, sep) {
			if c == "T{" {
				inCell = true
				break
			}
			row = append(row, inline(c))
		}
		if !inCell {
			rows = append(rows, row)
			row = nil
		}
	}
	var out []Para
	for _, r := range rows {
		out = append(out, Para{Indent: indent, Text: strings.Join(r, " │ "), Pre: true})
	}
	return out
}
