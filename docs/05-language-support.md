# Language Support

Yogilib is a trilingual archive serving content in **English**, **Nepali (नेपाली)**, and **Sanskrit (संस्कृत)**. This document covers how each language is handled in the browser, the encoding systems used in historical texts, and the conversion tools built into the site.

---

## Script: Unicode Devanagari

Both Nepali and Sanskrit use **Unicode Devanagari** (Unicode block U+0900–U+097F). This means:

- A single font (Hind, Noto Sans Devanagari) renders both languages correctly
- Text is searchable, copy-pasteable, and screen-reader accessible
- No special fonts or plugins needed in the browser
- The same `<textarea>` and Quill editor work for both

Sanskrit additionally uses some extended Devanagari characters:

| Character | Unicode | Usage |
|-----------|---------|-------|
| ं (anusvara) | U+0902 | Nasal marker (`M` in ITRANS) |
| ः (visarga) | U+0903 | Breath marker (`H` in ITRANS) |
| ँ (chandrabindu) | U+0901 | Nasalisation |
| ् (virama/halant) | U+094D | Cancels inherent vowel, forms conjuncts |
| ऽ (avagraha) | U+093D | Elision (`.a` in ITRANS) |
| ॐ (OM) | U+0950 | Sacred symbol (`OM` in ITRANS) |
| । (danda) | U+0964 | Full stop (`|` in ITRANS / Preeti) |
| ॥ (double danda) | U+0965 | Section end (`||` in ITRANS) |

---

## Typing Nepali: Unicode IME

When a user types Nepali directly in the browser (title field, Quill editor), they use a **Unicode input method**:

- **macOS**: System Preferences → Keyboard → Input Sources → Nepali
- **Windows**: Settings → Time & Language → Language → Add Nepali → Keyboard: Nepali Traditional
- **Any OS**: [Google Input Tools](https://www.google.com/inputtools/) (browser extension) — supports transliteration (type `namaste` → `नमस्ते`)
- **Any OS**: [Keyman](https://keyman.com/) with Nepali keyboard layout

All text is stored and transmitted as UTF-8 Unicode. No conversion needed.

---

## Legacy Encoding: Preeti (Nepali)

**Preeti** is an ASCII-based font encoding for Nepali that was widely used before Unicode adoption. Text appears as Nepali only when rendered in the *Preeti* font; the underlying bytes are ASCII characters.

### Why it matters for Yogilib

Many digitised historical documents — old Word files, scanned OCR output, legacy websites — contain Preeti-encoded text. If copied directly into a Unicode field, the text looks garbled (e.g. `k'/n ;doGjoy` instead of योगदान).

### The converter: `preeti-unicode.js`

Located at `/static/js/preeti-unicode.js`. Exports `preetiToUnicode(str)`.

**How it works**: A complete character-level lookup table maps each ASCII Preeti keystroke to its Unicode Devanagari equivalent. Since Preeti is a single-byte-to-single-character mapping, no state machine is needed.

```
k  → क    g  → ग    r  → र    s  → स
'  → ं    ;  → ः    .  → ।    |  → ।
1  → १    2  → २    ...
```

### Usage in the contribute form

1. **Preeti tab** in the Script Converter panel: paste a block of Preeti text → click Convert → click Insert into Editor
2. **Preeti mode toggle** on the Nepali title / author fields: every keystroke is live-converted to Unicode

---

## Legacy Encoding: ITRANS (Sanskrit)

**ITRANS** (Indian Language Transliteration) is an ASCII-based encoding for Sanskrit (and other Indic languages) used extensively in academic texts, Usenet, mailing lists, and digital Sanskrit libraries.

### The converter: `itrans-unicode.js`

Located at `/static/js/itrans-unicode.js`. Exports `itransToUnicode(str)`.

**How it works**: A state-machine parser processes the input left-to-right using greedy longest-match against a token table. The two key states are:

- **After consonant**: the next vowel becomes a *matra* (vowel sign) rather than a standalone vowel; the next consonant inserts a *virama* (्) to form a conjunct cluster
- **Start / after vowel or space**: vowels are output in standalone form

```
namaH shivAya   →  नमः शिवाय
dharmaH         →  धर्मः
saMskRRitam     →  संस्कृतम्
OM              →  ॐ
kShetram        →  क्षेत्रम्
```

### ITRANS Quick Reference

| ITRANS | Devanagari | ITRANS | Devanagari |
|--------|-----------|--------|-----------|
| `a` | अ | `A` / `aa` | आ |
| `i` | इ | `I` / `ii` | ई |
| `u` | उ | `U` / `uu` | ऊ |
| `e` | ए | `ai` | ऐ |
| `o` | ओ | `au` | औ |
| `RRi` | ऋ | `M` | ं |
| `H` | ः | `OM` | ॐ |
| `k kh g gh` | क ख ग घ | `c ch j jh` | च छ ज झ |
| `T Th D Dh N` | ट ठ ड ढ ण | `t th d dh n` | त थ द ध न |
| `p ph b bh m` | प फ ब भ म | `y r l v` | य र ल व |
| `sh Sh s h` | श ष स ह | `kSh GY` | क्ष ज्ञ |
| `|` | । |  `||` | ॥ |

### IAST (International Alphabet of Sanskrit Transliteration)

IAST uses Latin characters with diacritics (ā ī ū ṛ ḥ ṃ ṭ ḍ ṇ ś ṣ). It is common in academic publications. A basic IAST→Devanagari character map is included in `itrans-unicode.js` as `IAST_MAP` for future use.

---

## Language Field on Documents

The contribute form collects:

| Field | Values |
|-------|--------|
| **Language (भाषा)** | English / नेपाली / संस्कृत / नेपाली+संस्कृत / Mixed / Other |
| **Script (लिपि)** | देवनागरी / Latin-Roman / Latin IAST / Mixed |

These are stored in the `documents` table (`lang`, `script` columns) and will enable language-based filtering and display in a future version.

---

## Devanagari Numerals

Both Preeti and ITRANS converters output Devanagari digits:

| Latin | Devanagari |
|-------|-----------|
| 0 | ० |
| 1 | १ |
| 2 | २ |
| 3 | ३ |
| 4 | ४ |
| 5 | ५ |
| 6 | ६ |
| 7 | ७ |
| 8 | ८ |
| 9 | ९ |
