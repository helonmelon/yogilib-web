# Design System

## Typography

| Font | Usage | Source |
|------|-------|--------|
| **Labrada** | Site titles, headings, Yogi name | Google Fonts |
| **Hind** | Body text, Nepali/Sanskrit Devanagari content | Google Fonts |
| **Himalaya** | Legacy Nepali display (self-hosted woff/woff2) | `static/fonts/` |
| Monospace (system) | Code blocks, ITRANS input fields | Browser default |

Both **Hind** and the browser's built-in Noto Sans Devanagari (macOS/Windows) render Unicode Devanagari for Nepali and Sanskrit seamlessly — no additional font setup needed.

## Colour Palette

| Token | Value | Usage |
|-------|-------|-------|
| `--accent` | `#8b3a3a` | Primary brand colour — links, buttons, active states |
| Accent dark | `#6d2a2a` | Button hover |
| Preeti/converter amber | `#7a4a1e` / `#c9a07a` | Converter panel borders and text |
| Background | `#fafafa` | Form inputs, editor toolbar |
| Card | `#fff` | Form cards, editor container |
| Border | `#ddd` / `#e8ddd0` | Input borders, card borders |
| Flash OK | `#d4edda` / `#1d6a31` / border `#28a745` | Success banners |
| Flash Error | `#f8d7da` / `#842029` / border `#dc3545` | Error banners |
| Auto-date notice | `#f0f7ff` / `#2d5f8a` / border `#c3d9f5` | Informational blue badge |

## Layout

- **Max content width**: `900px` centred on the page (contribute page), narrower on document pages
- **Grid system**: CSS `display: grid` — 2-column (`fg-row`) and 3-column (`fg-row-3`) helpers
- **Responsive**: All multi-column grids collapse to single column at `max-width: 600px`

## Component Inventory

### Form Card (`.editor-card`)
White card with `1px solid #e8ddd0` border, `12px` radius, soft box shadow. Used to wrap the contribute form.

### Form Group (`.fg`)
Label (uppercase, 0.88rem, `#444`) + input/select. Focus state: border switches to `--accent`.

### Flash Banners (`.flash`, `.flash-ok`, `.flash-err`)
Full-width coloured banners with a left accent border. Rendered from `{{.Flash}}` / `{{.Error}}` in PageData.

### Drop Zone (`.drop-zone`)
Dashed amber border, centred icon + label. Supports drag-and-drop and click-to-browse. Turns green on file selection.

### Nepali Input Toggle (`.np-badge`)
Two pill buttons — **युनि** (Unicode direct mode) and **Preeti** (live Preeti→Unicode keystroke conversion) — overlaid on the right side of a text input. Used on title_np and orig_author_np fields.

### Section Divider (`.section-divider`)
Horizontal rule with centered label text. Used to visually group form sections.

### Auto-date Notice (`.auto-date-notice`)
Light blue info box with calendar icon. Explains upload date is auto-recorded and distinguishes it from the original document date.

### Author Section (`.author-section`)
Amber-tinted box grouping original author fields (English + Nepali) with a sticky note about uploader attribution.

### Editor Language Bar (`.editor-lang-bar`)
Pill-style buttons row: **English | नेपाली | संस्कृत (Sanskrit) | Mixed**. Switches the Quill editor `lang` attribute and disables spellcheck for non-English modes.

### Editor Tabs (`.editor-tabs`)
Two-button row: **Visual Editor** (Quill WYSIWYG) ↔ **HTML Source** (raw textarea). Syncs content bidirectionally on switch.

### Script Converter Panel (`.preeti-panel`)
Amber dashed-border panel with two tabs:
- **Preeti → Unicode** — for Nepali Preeti-encoded text
- **ITRANS → Devanagari** — for Sanskrit ITRANS ASCII transliteration

Each tab has: left textarea (input), arrow, right textarea (read-only output), and action buttons (Convert, Insert into Editor, Clear).

## Rich Text Editor (Quill.js)

**Version**: Quill 1.3.7 (Snow theme, CDN)  
**Toolbar groups**:

| Group | Options |
|-------|---------|
| Text structure | Heading (H1–H4), paragraph |
| Font | Font family, size (small/normal/large/huge) |
| Inline formatting | Bold, italic, underline, strikethrough |
| Colour | Text colour, background colour |
| Script | Subscript, superscript |
| Lists | Ordered, unordered, checklist |
| Indent | Increase / decrease |
| Alignment | Left, center, right, justify |
| Block | Blockquote, code block |
| Media | Hyperlink, image |
| Utility | Clear formatting |

All Devanagari text (Nepali + Sanskrit) renders correctly in the Quill editor body with the Hind font applied via `.ql-editor { font-family: 'Hind', sans-serif; }`.

## Page-specific Design Notes

### Contribute Page (`/upload`)
- Two-column title row: English title left, Nepali title (with toggle) right
- Category dropdown + Language + Script dropdowns in a 2-column row
- Blue auto-date notice before original date row (year / month / day — 3-column `2fr 1fr 1fr`)
- Author section in amber-tinted box with uploader placeholder note
- Quill editor with character counter and source tab
- Tabbed Script Converter panel
- Drag-and-drop file zone
- Submit button in the bottom-right

### Homepage (`/`)
- Document list with category filter tabs
- Search bar (passes `?q=` query param to server)
- Category tabs include: सबै किताब कागजात रेकर्ड पत्रिका **अंश** अन्य
