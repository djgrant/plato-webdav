package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Minimal markdown→HTML conversion so ephemeral .md documents are readable
// in Plato (which renders HTML but has no markdown engine). Covers the
// common subset: headings, paragraphs, emphasis, inline/fenced code,
// blockquotes, unordered/ordered lists, links, and horizontal rules.

// Plato's HTML renderer (its epub engine) supports only a small CSS subset;
// modern stylesheets (custom properties, grid/flex) can collapse a page to
// nothing. Stripping styles and scripts leaves semantic HTML it renders well.
var (
	reScript    = regexp.MustCompile(`(?is)<script\b.*?</script>`)
	reStyle     = regexp.MustCompile(`(?is)<style\b.*?</style>`)
	reStyleAttr = regexp.MustCompile(`(?i)\s+style\s*=\s*("[^"]*"|'[^']*')`)
	reLinkCSS   = regexp.MustCompile(`(?i)<link\b[^>]*>`)
	// Plato parses documents with a strict XML parser: an HTML5 void tag
	// like <meta charset="utf-8"> stays "open", swallowing the rest of the
	// document as children — the body ends up inside <head>, which the
	// built-in stylesheet hides, rendering a blank page. Self-closing the
	// void elements keeps the tree properly nested.
	reVoidTag = regexp.MustCompile(`(?i)<(meta|br|hr|img|input|source|wbr|col|area|base|embed|track|param)(\b[^>]*?)\s*/?>`)
)

func sanitizeHTML(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reStyleAttr.ReplaceAllString(s, "")
	s = reLinkCSS.ReplaceAllString(s, "")
	s = reVoidTag.ReplaceAllString(s, "<$1$2/>")
	return s
}

var (
	mdCode   = regexp.MustCompile("`([^`]+)`")
	mdBold   = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	mdItalic = regexp.MustCompile(`\*([^*]+)\*|\b_([^_]+)_\b`)
	mdLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	mdOlItem = regexp.MustCompile(`^\d+\.\s+`)
	// A GFM table separator row: pipes plus cells of dashes with optional
	// alignment colons, e.g. | :--- | ---: | :---: |.
	mdTableSep = regexp.MustCompile(`^\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$`)
)

// Kobo Clara-class devices are 6" / ~1072px wide, and we strip all CSS, so we
// can't constrain column widths. Beyond a few columns — or with long cells —
// a native <table> overflows the viewport with nowhere to scroll. Past these
// thresholds we fall back to a definition-list layout that always reflows.
const (
	maxTableCols  = 3
	maxTableWidth = 56 // characters across the widest row
)

// splitRow splits a table line into trimmed cell strings, tolerating the
// optional leading/trailing pipes GFM allows.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// isWideTable decides whether a table would overflow a narrow e-reader page.
func isWideTable(header []string, rows [][]string) bool {
	if len(header) > maxTableCols {
		return true
	}
	widest := rowWidth(header)
	for _, r := range rows {
		if w := rowWidth(r); w > widest {
			widest = w
		}
	}
	return widest > maxTableWidth
}

func rowWidth(cells []string) int {
	w := 0
	for _, c := range cells {
		w += len(c) + 3 // cell text plus " | " separators
	}
	return w
}

// renderTable emits a <table> for narrow tables, or a reflowing definition
// list for wide ones so the content stays legible on small screens.
func renderTable(b *strings.Builder, header []string, rows [][]string) {
	if !isWideTable(header, rows) {
		b.WriteString("<table>\n<thead>\n<tr>")
		for _, h := range header {
			b.WriteString("<th>" + mdInline(h) + "</th>")
		}
		b.WriteString("</tr>\n</thead>\n<tbody>\n")
		for _, r := range rows {
			b.WriteString("<tr>")
			for i := range header {
				b.WriteString("<td>" + mdInline(cell(r, i)) + "</td>")
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody>\n</table>\n")
		return
	}

	// Wide fallback: each row becomes a labelled block. The first column
	// heads the block; the rest become header/value pairs.
	for _, r := range rows {
		b.WriteString("<h4>" + mdInline(cell(r, 0)) + "</h4>\n<dl>\n")
		for i := 1; i < len(header); i++ {
			b.WriteString("<dt>" + mdInline(header[i]) + "</dt>")
			b.WriteString("<dd>" + mdInline(cell(r, i)) + "</dd>\n")
		}
		b.WriteString("</dl>\n")
	}
}

func cell(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

func mdInline(s string) string {
	s = html.EscapeString(s)
	s = mdCode.ReplaceAllString(s, "<code>$1</code>")
	s = mdBold.ReplaceAllString(s, "<strong>$1$2</strong>")
	s = mdItalic.ReplaceAllString(s, "<em>$1$2</em>")
	s = mdLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	return s
}

func markdownToHTML(md, title string) string {
	var b strings.Builder
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")

	var para []string
	list := "" // "ul", "ol" or ""
	inCode, inQuote := false, false

	flushPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + mdInline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	closeQuote := func() {
		if inQuote {
			b.WriteString("</blockquote>\n")
			inQuote = false
		}
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// A table is a header row followed by a separator row of dashes.
		if !inCode && strings.Contains(trimmed, "|") &&
			i+1 < len(lines) && mdTableSep.MatchString(strings.TrimSpace(lines[i+1])) {
			flushPara()
			closeList()
			closeQuote()
			header := splitRow(trimmed)
			var rows [][]string
			i += 2 // skip header and separator
			for i < len(lines) {
				rt := strings.TrimSpace(lines[i])
				if rt == "" || !strings.Contains(rt, "|") {
					break
				}
				rows = append(rows, splitRow(rt))
				i++
			}
			i-- // loop will re-increment
			renderTable(&b, header, rows)
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			flushPara()
			closeList()
			closeQuote()
			if inCode {
				b.WriteString("</code></pre>\n")
			} else {
				b.WriteString("<pre><code>")
			}
			inCode = !inCode
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(line) + "\n")
			continue
		}

		switch {
		case trimmed == "":
			flushPara()
			closeList()
			closeQuote()
		case strings.HasPrefix(trimmed, "#"):
			flushPara()
			closeList()
			closeQuote()
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' && level < 6 {
				level++
			}
			text := strings.TrimSpace(trimmed[level:])
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, mdInline(text), level)
		case trimmed == "---" || trimmed == "***" || trimmed == "___":
			flushPara()
			closeList()
			closeQuote()
			b.WriteString("<hr/>\n")
		case strings.HasPrefix(trimmed, ">"):
			flushPara()
			closeList()
			if !inQuote {
				b.WriteString("<blockquote>\n")
				inQuote = true
			}
			b.WriteString("<p>" + mdInline(strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))) + "</p>\n")
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			flushPara()
			closeQuote()
			if list != "ul" {
				closeList()
				b.WriteString("<ul>\n")
				list = "ul"
			}
			b.WriteString("<li>" + mdInline(trimmed[2:]) + "</li>\n")
		case mdOlItem.MatchString(trimmed):
			flushPara()
			closeQuote()
			if list != "ol" {
				closeList()
				b.WriteString("<ol>\n")
				list = "ol"
			}
			b.WriteString("<li>" + mdInline(mdOlItem.ReplaceAllString(trimmed, "")) + "</li>\n")
		default:
			closeList()
			closeQuote()
			para = append(para, trimmed)
		}
	}
	flushPara()
	closeList()
	closeQuote()
	if inCode {
		b.WriteString("</code></pre>\n")
	}

	return "<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"/><title>" +
		html.EscapeString(title) + "</title></head><body>\n" + b.String() + "</body></html>\n"
}
