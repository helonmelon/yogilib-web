// wiki.go — wiki-link parser, slug helper, TOC extractor
// Part of package main (same package as main.go).
package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// LinkRef holds parsed wiki-link data for persistence in the links table.
type LinkRef struct {
	ToDocID *int   // nil when target document does not exist (wanted link)
	ToSlug  string // normalised target slug
	Anchor  string // display text used as anchor
}

// TOCEntry represents one heading in the auto-generated table of contents.
type TOCEntry struct {
	Level int    // 2 or 3
	Text  string // plain-text content of the heading
	ID    string // slugified, injected as id="..." attribute
}

// ---------------------------------------------------------------------------
// Slug helper
// ---------------------------------------------------------------------------

// slugify converts s to a URL-safe slug:
//   - Unicode letters and digits (including Devanagari) pass through
//   - ASCII is lower-cased
//   - whitespace, hyphens, underscores → single "-"
//   - everything else (punctuation) is stripped
//   - leading/trailing hyphens are trimmed
func slugify(s string) string {
	// strings.ToLower handles ASCII; Devanagari has no case so it is unchanged.
	s = strings.ToLower(s)
	var b strings.Builder
	prevHyphen := true // start true to trim leading hyphens
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			// IsMark covers Devanagari combining vowel signs (matras) and
			// nukta/virama — dropping them mangles words like
			// "सुगौली" → "सगल".
			b.WriteRune(r)
			prevHyphen = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		// everything else: strip
		}
	}
	result := b.String()
	// Trim trailing hyphen that may have been added
	return strings.TrimRight(result, "-")
}

// ---------------------------------------------------------------------------
// Wiki-link parser
// ---------------------------------------------------------------------------

// wikiLinkRe matches [[Target]] and [[Target|Display]].
var wikiLinkRe = regexp.MustCompile(`\[\[([^\]|]+?)(?:\|([^\]]+?))?\]\]`)

// codeBlockRe matches <pre>…</pre> and <code>…</code> blocks (dot-all, case-insensitive).
var codeBlockRe = regexp.MustCompile(`(?si)(<(?:pre|code)[^>]*>.*?</(?:pre|code)>)`)

// innerTagRe strips HTML tags from heading inner HTML.
var innerTagRe = regexp.MustCompile(`<[^>]+>`)

// headingRe matches <h2> and <h3> tags with any attributes, capturing inner HTML.
var headingRe = regexp.MustCompile(`(?si)<(h[23])([^>]*)>(.*?)</h[23]>`)

// slugLookupFn is the type of the slug → docID lookup function.
// Returns nil if the document is not found.
type slugLookupFn func(slug string) *int

// dbSlugLookup is the production lookup that queries the live database.
func dbSlugLookup(slug string) *int {
	var id int
	err := db.QueryRow(`SELECT id FROM documents WHERE slug = ?`, slug).Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}

// parseWikiLinks rewrites [[Target]] / [[Target|Display]] patterns in htmlBody
// into HTML anchor tags, skipping content inside <pre> and <code> blocks.
//
// It uses the global database (via dbSlugLookup) to resolve slugs.
// Returns the rewritten HTML and a slice of LinkRef values for persistence.
func parseWikiLinks(htmlBody string) (string, []LinkRef) {
	return parseWikiLinksWithLookup(htmlBody, dbSlugLookup)
}

// parseWikiLinksWithLookup is the testable core of parseWikiLinks.
func parseWikiLinksWithLookup(htmlBody string, lookup slugLookupFn) (string, []LinkRef) {
	var refs []LinkRef

	// Split the HTML at code/pre block boundaries so we never touch their contents.
	parts := codeBlockRe.Split(htmlBody, -1)
	blocks := codeBlockRe.FindAllString(htmlBody, -1)

	var result strings.Builder
	for i, part := range parts {
		// Apply wiki-link substitution only to non-code regions.
		processed := wikiLinkRe.ReplaceAllStringFunc(part, func(match string) string {
			sub := wikiLinkRe.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			target := strings.TrimSpace(sub[1])
			display := target
			if len(sub) >= 3 && sub[2] != "" {
				display = strings.TrimSpace(sub[2])
			}
			slug := slugify(target)

			docID := lookup(slug)
			if docID != nil {
				id := *docID
				refs = append(refs, LinkRef{ToDocID: &id, ToSlug: slug, Anchor: display})
				return fmt.Sprintf(`<a class="wikilink" href="/document/%d">%s</a>`, id, display)
			}
			// Target document not found — render as a "wanted" (red) link.
			refs = append(refs, LinkRef{ToDocID: nil, ToSlug: slug, Anchor: display})
			return fmt.Sprintf(`<a class="wikilink wanted" href="/wanted/%s">%s</a>`, slug, display)
		})

		result.WriteString(processed)
		if i < len(blocks) {
			result.WriteString(blocks[i])
		}
	}
	return result.String(), refs
}

// ---------------------------------------------------------------------------
// TOC extractor
// ---------------------------------------------------------------------------

// extractTOC scans htmlBody for <h2> and <h3> headings, injects id="…"
// attributes (slugified from the heading plain-text), and returns the modified
// HTML together with a slice of TOCEntry values.
//
// If a heading already has an id attribute it is left unchanged.
func extractTOC(htmlBody string) (string, []TOCEntry) {
	var entries []TOCEntry

	result := headingRe.ReplaceAllStringFunc(htmlBody, func(match string) string {
		sub := headingRe.FindStringSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		tag := sub[1]   // "h2" or "h3"
		attrs := sub[2] // existing attributes (may be empty)
		inner := sub[3] // raw inner HTML

		// Plain-text label for the TOC entry.
		text := strings.TrimSpace(innerTagRe.ReplaceAllString(inner, ""))
		if text == "" {
			return match
		}

		id := slugify(text)
		level := 2
		if tag == "h3" {
			level = 3
		}
		entries = append(entries, TOCEntry{Level: level, Text: text, ID: id})

		// Don't touch headings that already carry an id attribute.
		if strings.Contains(strings.ToLower(attrs), "id=") {
			return match
		}

		// Preserve existing attributes and append the new id.
		return fmt.Sprintf(`<%s%s id="%s">%s</%s>`, tag, attrs, id, inner, tag)
	})

	return result, entries
}
