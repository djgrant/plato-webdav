package main

import (
	"strings"
	"testing"
)

func TestNarrowTableRendersAsBorderedTable(t *testing.T) {
	md := "| Name | Age |\n| --- | --- |\n| Ann | 30 |\n| Bob | 25 |\n"
	got := markdownToHTML(md, "t")

	for _, want := range []string{
		`<table border="1"`, "<th><strong>Name</strong></th>",
		"<th><strong>Age</strong></th>", "<td>Ann</td>", "<td>30</td>",
		"<td>Bob</td>", "</table>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "|") {
		t.Errorf("raw pipes leaked into output:\n%s", got)
	}
}

func TestWideTableFallsBackToPerRowTables(t *testing.T) {
	// Four columns exceeds maxTableCols, triggering the vertical fallback.
	md := "| Name | Age | City | Job |\n| - | - | - | - |\n" +
		"| Ann | 30 | Leeds | Engineer |\n"
	got := markdownToHTML(md, "t")

	if strings.Contains(got, "<thead>") {
		t.Errorf("expected vertical fallback, got a header row:\n%s", got)
	}
	for _, want := range []string{
		"<h4>Ann</h4>", "<th><strong>Age</strong></th>", "<td>30</td>",
		"<th><strong>City</strong></th>", "<td>Leeds</td>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestCrampedThreeColumnFallsBack(t *testing.T) {
	// Three columns but wide content: too cramped side-by-side on a small
	// screen, so it must fall back to the vertical layout.
	md := "| Key | Default | Meaning |\n| - | - | - |\n" +
		"| server-url | (required) | WebDAV collection URL for the share you sync |\n"
	got := markdownToHTML(md, "t")
	if strings.Contains(got, "<thead>") {
		t.Errorf("expected fallback for cramped 3-column table, got:\n%s", got)
	}
	if !strings.Contains(got, "<th><strong>Meaning</strong></th>") {
		t.Errorf("expected label column, got:\n%s", got)
	}
}

func TestEmptyTopLeftMatrix(t *testing.T) {
	// The comparison-matrix shape: empty top-left, row labels in column one.
	md := "| | pi | cc |\n|---|---|---|\n" +
		"| channel | a real conversation message, persisted and re-sent to the LLM | additionalContext on UserPromptSubmit |\n"
	got := markdownToHTML(md, "t")

	if !strings.Contains(got, "<h4>channel</h4>") {
		t.Errorf("expected row label as heading, got:\n%s", got)
	}
	for _, want := range []string{
		"<th><strong>pi</strong></th>",
		"<th><strong>cc</strong></th>", "<td>additionalContext on UserPromptSubmit</td>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRaggedRowPadsMissingCells(t *testing.T) {
	md := "| A | B |\n| - | - |\n| only |\n"
	got := markdownToHTML(md, "t")
	if !strings.Contains(got, "<td>only</td>") || !strings.Contains(got, "<td></td>") {
		t.Errorf("expected padded empty cell, got:\n%s", got)
	}
}
