// wiki_test.go — unit tests for slugify, parseWikiLinks, extractTOC
package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// slugify tests
// ---------------------------------------------------------------------------

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// ASCII basics
		{"Hello World", "hello-world"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"Treaty of Sugowlee", "treaty-of-sugowlee"},
		{"Multiple   spaces", "multiple-spaces"},
		{"already-hyphenated", "already-hyphenated"},
		// Punctuation stripped
		{"Hello, World!", "hello-world"},
		{"What? No. Really...", "what-no-really"},
		{`He said "hi"`, "he-said-hi"},
		{"[brackets] (parens) {braces}", "brackets-parens-braces"},
		// Devanagari — letters pass through; no case change
		{"योगी नरहरिनाथ", "योगी-नरहरिनाथ"},
		{"सुगौली सन्धि", "सुगौली-सन्धि"},
		// Mixed ASCII + Devanagari
		{"Yogi नरहरिनाथ 1971", "yogi-नरहरिनाथ-1971"},
		// Edge cases
		{"", ""},
		{"---", ""},
		{"   ", ""},
		{"123", "123"},
		{"camelCase", "camelcase"},
	}

	for _, tc := range tests {
		got := slugify(tc.in)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseWikiLinks tests
// ---------------------------------------------------------------------------

// mockLookup simulates a slug → docID map without a real database.
func mockLookup(known map[string]int) slugLookupFn {
	return func(slug string) *int {
		if id, ok := known[slug]; ok {
			return &id
		}
		return nil
	}
}

func TestParseWikiLinks_Resolved(t *testing.T) {
	known := map[string]int{"treaty-of-sugowlee": 42}
	lookup := mockLookup(known)

	in := `See the [[Treaty of Sugowlee]] for details.`
	out, refs := parseWikiLinksWithLookup(in, lookup)

	want := `See the <a class="wikilink" href="/document/42">Treaty of Sugowlee</a> for details.`
	if out != want {
		t.Errorf("output mismatch\ngot:  %s\nwant: %s", out, want)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].ToDocID == nil || *refs[0].ToDocID != 42 {
		t.Errorf("ref ToDocID: got %v, want 42", refs[0].ToDocID)
	}
	if refs[0].ToSlug != "treaty-of-sugowlee" {
		t.Errorf("ref ToSlug: got %q, want \"treaty-of-sugowlee\"", refs[0].ToSlug)
	}
}

func TestParseWikiLinks_Unresolved(t *testing.T) {
	lookup := mockLookup(map[string]int{}) // empty — nothing exists

	in := `See [[Missing Page]] here.`
	out, refs := parseWikiLinksWithLookup(in, lookup)

	if !strings.Contains(out, `class="wikilink wanted"`) {
		t.Errorf("expected wanted link class, got: %s", out)
	}
	if !strings.Contains(out, `href="/wanted/missing-page"`) {
		t.Errorf("expected wanted href, got: %s", out)
	}
	if len(refs) != 1 || refs[0].ToDocID != nil {
		t.Errorf("expected 1 ref with nil ToDocID, got %+v", refs)
	}
}

func TestParseWikiLinks_DisplayAlias(t *testing.T) {
	known := map[string]int{"treaty-of-sugowlee": 7}
	lookup := mockLookup(known)

	in := `[[Treaty of Sugowlee|the Treaty]]`
	out, refs := parseWikiLinksWithLookup(in, lookup)

	want := `<a class="wikilink" href="/document/7">the Treaty</a>`
	if out != want {
		t.Errorf("got: %s\nwant: %s", out, want)
	}
	if refs[0].Anchor != "the Treaty" {
		t.Errorf("anchor: got %q, want \"the Treaty\"", refs[0].Anchor)
	}
}

func TestParseWikiLinks_SkipsCodeBlock(t *testing.T) {
	lookup := mockLookup(map[string]int{"foo": 1})

	in := `Before <code>[[Foo]]</code> after [[Foo]] end.`
	out, refs := parseWikiLinksWithLookup(in, lookup)

	// The [[Foo]] inside <code> must not be rewritten.
	if !strings.Contains(out, "<code>[[Foo]]</code>") {
		t.Errorf("code block was modified: %s", out)
	}
	// The [[Foo]] outside must be rewritten.
	if !strings.Contains(out, `href="/document/1"`) {
		t.Errorf("outside link not resolved: %s", out)
	}
	// Only one ref — the one outside <code>.
	if len(refs) != 1 {
		t.Errorf("expected 1 ref, got %d", len(refs))
	}
}

func TestParseWikiLinks_SkipsPreBlock(t *testing.T) {
	lookup := mockLookup(map[string]int{"foo": 1})

	in := "<pre>\n[[Foo]]\n</pre>\n[[Foo]]"
	out, refs := parseWikiLinksWithLookup(in, lookup)

	if !strings.Contains(out, "<pre>\n[[Foo]]\n</pre>") {
		t.Errorf("pre block was modified: %s", out)
	}
	if len(refs) != 1 {
		t.Errorf("expected 1 ref, got %d", len(refs))
	}
}

func TestParseWikiLinks_MultipleLinks(t *testing.T) {
	known := map[string]int{"page-a": 1, "page-b": 2}
	lookup := mockLookup(known)

	in := `[[Page A]] and [[Page B]] and [[Page C]].`
	_, refs := parseWikiLinksWithLookup(in, lookup)

	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	// First two resolved, third not.
	if refs[0].ToDocID == nil {
		t.Error("refs[0] should be resolved")
	}
	if refs[1].ToDocID == nil {
		t.Error("refs[1] should be resolved")
	}
	if refs[2].ToDocID != nil {
		t.Error("refs[2] should be unresolved (wanted)")
	}
}

func TestParseWikiLinks_DevanagariTarget(t *testing.T) {
	known := map[string]int{"सुगौली-सन्धि": 99}
	lookup := mockLookup(known)

	in := `[[सुगौली सन्धि]]`
	out, refs := parseWikiLinksWithLookup(in, lookup)

	if !strings.Contains(out, `href="/document/99"`) {
		t.Errorf("Devanagari slug not resolved: %s", out)
	}
	if len(refs) != 1 || refs[0].ToSlug != "सुगौली-सन्धि" {
		t.Errorf("unexpected refs: %+v", refs)
	}
}

// ---------------------------------------------------------------------------
// extractTOC tests
// ---------------------------------------------------------------------------

func TestExtractTOC_NoHeadings(t *testing.T) {
	in := `<p>Just a paragraph.</p>`
	out, entries := extractTOC(in)

	if out != in {
		t.Errorf("HTML was modified unexpectedly: %s", out)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestExtractTOC_OnlyH2(t *testing.T) {
	in := `<h2>First Section</h2><p>body</p><h2>Second Section</h2>`
	out, entries := extractTOC(in)

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Level != 2 || entries[0].Text != "First Section" || entries[0].ID != "first-section" {
		t.Errorf("entry 0: %+v", entries[0])
	}
	if entries[1].Level != 2 || entries[1].Text != "Second Section" || entries[1].ID != "second-section" {
		t.Errorf("entry 1: %+v", entries[1])
	}
	if !strings.Contains(out, `id="first-section"`) {
		t.Errorf("id not injected: %s", out)
	}
}

func TestExtractTOC_MixedH2H3(t *testing.T) {
	in := `<h2>Chapter One</h2><h3>Sub-section A</h3><h2>Chapter Two</h2><h3>Sub-section B</h3>`
	_, entries := extractTOC(in)

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Level != 2 {
		t.Errorf("entry 0 level: got %d, want 2", entries[0].Level)
	}
	if entries[1].Level != 3 {
		t.Errorf("entry 1 level: got %d, want 3", entries[1].Level)
	}
}

func TestExtractTOC_HeadingWithInnerHTML(t *testing.T) {
	// Heading contains a <span> — plain text should be used for slug/text.
	in := `<h2><span class="x">Hello <em>World</em></span></h2>`
	_, entries := extractTOC(in)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Text != "Hello World" {
		t.Errorf("text: got %q, want \"Hello World\"", entries[0].Text)
	}
	if entries[0].ID != "hello-world" {
		t.Errorf("id: got %q, want \"hello-world\"", entries[0].ID)
	}
}

func TestExtractTOC_ExistingID(t *testing.T) {
	// If heading already has id=, leave it untouched.
	in := `<h2 id="custom-id">My Heading</h2>`
	out, entries := extractTOC(in)

	if out != in {
		t.Errorf("heading with existing id was modified: %s", out)
	}
	// Still captured in TOC.
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}
