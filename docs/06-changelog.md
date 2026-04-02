# Changelog

All notable changes to Yogilib Web are documented here.

---

## [Unreleased] — Session: 2026-04-01

### Contribute Page — Rich Text Editor (`/upload`)

**Added** full-featured online document editor to the contribute page, replacing the plain `<textarea>` description field.

**Editor**: [Quill.js](https://quilljs.com/) v1.3.7 (Snow theme, loaded from CDN, no build step)

**Toolbar capabilities**:
- Headings H1–H4 (like MS Word styles)
- Font family and size selection
- Bold, italic, underline, strikethrough
- Text colour and background colour
- Superscript / subscript
- Ordered lists, bullet lists, checklists
- Indentation (increase/decrease)
- Text alignment (left/center/right/justify)
- Blockquote and code block
- Hyperlink and image insertion
- Clear all formatting

**Additional editor features**:
- **Visual ↔ HTML Source tabs** — toggle between WYSIWYG and raw HTML editing; content syncs bidirectionally
- **Language mode bar** — English / नेपाली / संस्कृत (Sanskrit) / Mixed — sets `lang` attribute on the editor and disables spellcheck for Devanagari modes
- **Character counter** displayed below the editor

---

### Preeti → Unicode Converter

**Added** `static/js/preeti-unicode.js` — a complete Preeti-to-Unicode character lookup table.

**Why**: Many historical Nepali documents were typed using the Preeti font encoding (ASCII characters rendered as Nepali glyphs). These cannot be directly used in a Unicode system without conversion.

**Features**:
- Full Preeti character map (consonants, vowels, matras, punctuation, Devanagari digits)
- Live conversion as user types (input event listener)
- **Live keystroke mode** on the Nepali title field — every Preeti key press is converted to Unicode in real time
- Paste-and-convert mode in the converter panel
- "Insert into Editor" button pushes converted text directly into the Quill editor at the cursor

---

### ITRANS → Devanagari Converter

**Added** `static/js/itrans-unicode.js` — a stateful ITRANS parser for Sanskrit (and Nepali) transliteration.

**Why**: Sanskrit texts are commonly encoded in ITRANS (ASCII-based transliteration). Examples: `dharma` → धर्म, `namaH` → नमः, `OM` → ॐ.

**Implementation**: Greedy left-to-right token matching with a two-state machine (`afterConsonant` flag):
- Consonant + vowel → consonant with matra
- Consonant + consonant → consonant + virama + consonant (conjunct)
- Handles: all standard consonants, aspirated pairs, retroflex series, diphthongs, anusvara, visarga, chandrabindu, avagraha, danda, double danda, Devanagari digits, conjunct shortcuts (kSh, GY, etc.)

---

### Script Converters Panel (Tabbed)

**Changed** the old single Preeti converter panel into a **tabbed panel** with two modes:

- 🇳 **Preeti → Unicode** tab (Nepali)
- 🕉️ **ITRANS → Devanagari** tab (Sanskrit)

Each tab has independent input/output textareas, Convert, Insert into Editor, and Clear buttons.

---

### Language & Script Fields on Contribute Form

**Added** two new metadata fields to the contribute form (below the Category dropdown):

| Field | Options |
|-------|---------|
| **भाषा / Language** | English, नेपाली, संस्कृत, नेपाली+संस्कृत, Mixed, Other |
| **लिपि / Script** | देवनागरी, Latin/Roman, Latin IAST, Mixed |

---

### Original Date Fields

**Changed** the single "Year" text field to a structured **three-part original date**:

- **Year** (free text — accepts `1815` or `१८७२`)
- **Month** (dropdown with English + Nepali month names)
- **Day** (number input, 1–31)

**Added** an informational blue notice banner explaining that the upload date is recorded automatically on submission, and the these fields are for the document's original date.

**Form field names**: `orig_year`, `orig_month`, `orig_day` (vs. the auto-recorded `created_at` in the DB)

---

### Author Fields

**Added** an "Authorship / लेखक विवरण" section with:

- **Original Author** (English) — free text, e.g. "Yogi Narharinath"
- **Original Author (नेपाली)** — Unicode Nepali field with युनि/Preeti toggle
- **Contributor placeholder note** — explains the logged-in user will be auto-recorded as the uploader once authentication is enabled

**Form field names**: `orig_author`, `orig_author_np`

---

### Category: अंश (Excerpt) Added

**Added** "अंश (Excerpt)" as a new category option in both:
- The contribute form (`/upload`) category dropdown
- The server-side `docCategories` slice in `main.go` (appears as a filter tab on the homepage)

**Updated** `docCategories`:
```go
// Before
[]string{"सबै", "किताब", "कागजात", "रेकर्ड", "पत्रिका", "अन्य"}

// After
[]string{"सबै", "किताब", "कागजात", "रेकर्ड", "पत्रिका", "अंश", "अन्य"}
```

---

### Mock Data: Removed Memorandum Entry

**Removed** the "Memorandum of 8th December 1816" placeholder document from all mock data functions:
- `mockDocuments()` — removed document ID `"2"`
- `mockExcerpts()` — removed `memorandum-1816` slug
- `mockExcerptBySlug()` — removed `memorandum-1816` map entry

**Kept**: "Treaty of Sugowlee" (ID `"1"`) as the sole example document.

---

### Documentation (`docs/` folder)

**Added** a `docs/` directory with the following files:

| File | Contents |
|------|----------|
| `01-getting-started.md` | Requirements, running the server, project layout, adding new pages |
| `02-architecture.md` | Request lifecycle, core types, routing table, template system, mock data layer |
| `03-design-system.md` | Typography, colour palette, layout, all UI components, Quill toolbar |
| `04-backend-integration.md` | Full database schema, handler TODO map, file upload flow, auth, search |
| `05-language-support.md` | Unicode, Nepali typing, Preeti encoding, ITRANS, IAST, Sanskrit specifics |
| `06-changelog.md` | This file |

**Updated** `README.md` to serve as a concise index pointing to the `docs/` folder.
