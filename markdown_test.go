package main

import (
	"strings"
	"testing"
)

func TestNarrowTableRendersAsTable(t *testing.T) {
	md := "| Name | Age |\n| --- | --- |\n| Ann | 30 |\n| Bob | 25 |\n"
	got := markdownToHTML(md, "t")

	for _, want := range []string{
		"<table>", "<th>Name</th>", "<th>Age</th>",
		"<td>Ann</td>", "<td>30</td>", "<td>Bob</td>", "</table>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "|") {
		t.Errorf("raw pipes leaked into output:\n%s", got)
	}
}

func TestWideTableFallsBackToDefinitionList(t *testing.T) {
	// Four columns exceeds maxTableCols, triggering the reflow fallback.
	md := "| Name | Age | City | Job |\n| - | - | - | - |\n" +
		"| Ann | 30 | Leeds | Engineer |\n"
	got := markdownToHTML(md, "t")

	if strings.Contains(got, "<table>") {
		t.Errorf("expected definition-list fallback, got a table:\n%s", got)
	}
	for _, want := range []string{
		"<h4>Ann</h4>", "<dl>", "<dt>Age</dt>", "<dd>30</dd>",
		"<dt>City</dt>", "<dd>Leeds</dd>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestModerateThreeColumnStaysTable(t *testing.T) {
	// A 3-column table with a fairly long cell should stay a table: native
	// wrapping handles it, so it must not fall back to a definition list.
	md := "| Key | Default | Meaning |\n| - | - | - |\n" +
		"| server-url | (required) | WebDAV collection URL for the share you sync |\n"
	got := markdownToHTML(md, "t")
	if !strings.Contains(got, "<table>") {
		t.Errorf("expected a table for moderate 3-column content, got:\n%s", got)
	}
}

func TestWideTableByCellWidth(t *testing.T) {
	// Two columns, but a cell longer than maxCellLen triggers the fallback.
	long := strings.Repeat("x", maxCellLen+10)
	md := "| Name | Notes |\n| - | - |\n| Ann | " + long + " |\n"
	got := markdownToHTML(md, "t")

	if strings.Contains(got, "<table>") {
		t.Errorf("expected fallback for wide cell, got a table:\n%s", got)
	}
	if !strings.Contains(got, "<dt>Notes</dt>") {
		t.Errorf("expected definition list, got:\n%s", got)
	}
}

func TestRaggedRowPadsMissingCells(t *testing.T) {
	md := "| A | B |\n| - | - |\n| only |\n"
	got := markdownToHTML(md, "t")
	if !strings.Contains(got, "<td>only</td>") || !strings.Contains(got, "<td></td>") {
		t.Errorf("expected padded empty cell, got:\n%s", got)
	}
}
