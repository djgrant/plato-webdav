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

var (
	mdCode   = regexp.MustCompile("`([^`]+)`")
	mdBold   = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	mdItalic = regexp.MustCompile(`\*([^*]+)\*|\b_([^_]+)_\b`)
	mdLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	mdOlItem = regexp.MustCompile(`^\d+\.\s+`)
)

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

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

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
