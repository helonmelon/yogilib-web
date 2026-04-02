/**
 * itrans-unicode.js
 * Converts ITRANS-encoded Sanskrit/Nepali (ASCII) to Unicode Devanagari.
 *
 * ITRANS reference: https://www.aczoom.com/itrans/
 * Algorithm: greedy left-to-right token matching with state machine
 * (tracks whether we're immediately after a consonant to decide
 *  between standalone vowel form vs. matra form, and to insert
 *  virama ् for consonant clusters).
 */

// ---------------------------------------------------------------------------
// Token table  [itrans_string, standalone_unicode, matra_unicode, type]
// IMPORTANT: sorted longest-first to ensure greedy matching.
//   type: 'consonant' | 'vowel' | 'matra' | 'special' | 'ignore'
// ---------------------------------------------------------------------------
const ITRANS_TOKENS = [
  // ── Special multi-char forms (must be first) ─────────────────────────
  ['OM',   'ॐ',  '',   'special'],
  ['AUM',  'ॐ',  '',   'special'],
  ['|',    '।',  '',   'special'],
  ['||',   '॥',  '',   'special'],
  ['.a',   'ऽ',  '',   'special'],  // avagraha

  // ── Conjunct consonant shortcuts ──────────────────────────────────────
  ['kSh',  'क्ष', '', 'consonant'],
  ['ksh',  'क्ष', '', 'consonant'],
  ['kS',   'क्ष', '', 'consonant'],
  ['GY',   'ज्ञ', '', 'consonant'],
  ['jnY',  'ज्ञ', '', 'consonant'],
  ['dny',  'ज्ञ', '', 'consonant'],
  ['JN',   'ञ',  '',   'consonant'],

  // ── Aspirated/digraph consonants ──────────────────────────────────────
  ['N^',   'ङ',  '',   'consonant'],
  ['~n',   'ञ',  '',   'consonant'],
  ['chh',  'छ',  '',   'consonant'],
  ['Ch',   'छ',  '',   'consonant'],
  ['ch',   'च',  '',   'consonant'],
  ['kh',   'ख',  '',   'consonant'],
  ['gh',   'घ',  '',   'consonant'],
  ['jh',   'झ',  '',   'consonant'],
  ['Th',   'ठ',  '',   'consonant'],
  ['Dh',   'ढ',  '',   'consonant'],
  ['th',   'थ',  '',   'consonant'],
  ['dh',   'ध',  '',   'consonant'],
  ['ph',   'फ',  '',   'consonant'],
  ['bh',   'भ',  '',   'consonant'],
  ['Sh',   'ष',  '',   'consonant'],
  ['sh',   'श',  '',   'consonant'],

  // ── Extended vowels (long sequences first) ────────────────────────────
  ['RRi',  'ऋ',  'ृ',  'vowel'],
  ['R^i',  'ऋ',  'ृ',  'vowel'],
  ['RRI',  'ॠ',  'ॄ',  'vowel'],
  ['R^I',  'ॠ',  'ॄ',  'vowel'],
  ['LLi',  'ऌ',  'ॢ',  'vowel'],
  ['L^i',  'ऌ',  'ॢ',  'vowel'],
  ['LLI',  'ॡ',  'ॣ',  'vowel'],

  // ── Diphthongs (before single chars) ─────────────────────────────────
  ['ai',   'ऐ',  'ै',  'vowel'],
  ['au',   'औ',  'ौ',  'vowel'],
  ['aa',   'आ',  'ा',  'vowel'],
  ['ii',   'ई',  'ी',  'vowel'],
  ['uu',   'ऊ',  'ू',  'vowel'],

  // ── Anusvara / visarga / chandrabindu ─────────────────────────────────
  ['.m',   'ं',  'ं',  'matra'],
  ['.M',   'ं',  'ं',  'matra'],
  ['.n',   'ँ',  'ँ',  'matra'],
  ['.h',   'ः',  'ः',  'matra'],
  ['M',    'ं',  'ं',  'matra'],
  ['H',    'ः',  'ः',  'matra'],

  // ── Single vowels ─────────────────────────────────────────────────────
  ['A',    'आ',  'ा',  'vowel'],
  ['I',    'ई',  'ी',  'vowel'],
  ['U',    'ऊ',  'ू',  'vowel'],
  ['a',    'अ',  '',   'vowel'],  // inherent 'a' — matra is empty (suppress output)
  ['i',    'इ',  'ि',  'vowel'],
  ['u',    'उ',  'ु',  'vowel'],
  ['e',    'ए',  'े',  'vowel'],
  ['E',    'ए',  'े',  'vowel'],
  ['o',    'ओ',  'ो',  'vowel'],
  ['O',    'ओ',  'ो',  'vowel'],

  // ── Single consonants ─────────────────────────────────────────────────
  ['k',    'क',  '',   'consonant'],
  ['g',    'ग',  '',   'consonant'],
  ['c',    'च',  '',   'consonant'],
  ['j',    'ज',  '',   'consonant'],
  ['T',    'ट',  '',   'consonant'],
  ['D',    'ड',  '',   'consonant'],
  ['N',    'ण',  '',   'consonant'],
  ['t',    'त',  '',   'consonant'],
  ['d',    'द',  '',   'consonant'],
  ['n',    'न',  '',   'consonant'],
  ['p',    'प',  '',   'consonant'],
  ['f',    'फ',  '',   'consonant'],
  ['b',    'ब',  '',   'consonant'],
  ['m',    'म',  '',   'consonant'],
  ['y',    'य',  '',   'consonant'],
  ['r',    'र',  '',   'consonant'],
  ['l',    'ल',  '',   'consonant'],
  ['L',    'ळ',  '',   'consonant'],
  ['v',    'व',  '',   'consonant'],
  ['w',    'व',  '',   'consonant'],
  ['S',    'श',  '',   'consonant'],
  ['s',    'स',  '',   'consonant'],
  ['h',    'ह',  '',   'consonant'],

  // ── Devanagari digits ─────────────────────────────────────────────────
  ['0', '०', '', 'special'], ['1', '१', '', 'special'],
  ['2', '२', '', 'special'], ['3', '३', '', 'special'],
  ['4', '४', '', 'special'], ['5', '५', '', 'special'],
  ['6', '६', '', 'special'], ['7', '७', '', 'special'],
  ['8', '८', '', 'special'], ['9', '९', '', 'special'],
];

// Build a sorted version (longest itrans string first) for greedy matching
const SORTED_TOKENS = [...ITRANS_TOKENS].sort((a, b) => b[0].length - a[0].length);

const VIRAMA = '्';

/**
 * Convert an ITRANS-encoded Sanskrit/Nepali string to Unicode Devanagari.
 *
 * State machine:
 *   afterConsonant = true  → last token output was a consonant;
 *                            next vowel becomes a matra, next consonant gets a virama prefix.
 *   afterConsonant = false → start state; vowels are output in standalone form.
 *
 * @param {string} input  ITRANS text (e.g. "dharma", "namaH", "saMskRRitam")
 * @returns {string}      Unicode Devanagari
 */
function itransToUnicode(input) {
  if (!input) return '';
  let out = '';
  let i = 0;
  let afterConsonant = false;

  while (i < input.length) {
    // ── Try to match a known token (greedy) ────────────────────
    let matched = false;
    for (const [itrans, standalone, matra, type] of SORTED_TOKENS) {
      if (input.substr(i, itrans.length) === itrans) {
        switch (type) {
          case 'consonant':
            if (afterConsonant) {
              out += VIRAMA; // form conjunct cluster
            }
            out += standalone;
            afterConsonant = true;
            break;

          case 'vowel':
            if (afterConsonant) {
              // matra form — '' means inherent 'a', output nothing
              out += matra;
              afterConsonant = false;
            } else {
              out += standalone; // standalone vowel
            }
            break;

          case 'matra':
            // anusvara, visarga, chandrabindu — always output as-is
            out += standalone;
            afterConsonant = false;
            break;

          case 'special':
            if (afterConsonant) {
              // No vowel followed this consonant — leave inherent 'a'
              afterConsonant = false;
            }
            out += standalone;
            break;
        }
        i += itrans.length;
        matched = true;
        break;
      }
    }

    if (!matched) {
      // Pass-through character (space, newline, punctuation, {}, …)
      const ch = input[i];
      // Braces are used in some ITRANS texts to delimit Sanskrit from other text
      if (ch === '{' || ch === '}') { i++; continue; }
      if (afterConsonant && (ch === ' ' || ch === '\n' || ch === '\t')) {
        afterConsonant = false; // word boundary — inherent 'a' is fine
      }
      out += ch;
      if (' \n\t\r'.includes(ch)) afterConsonant = false;
      i++;
    }
  }
  return out;
}

// ── IAST → Devanagari (simple character-level map) ──────────────────────────
// IAST uses precomposed Unicode Latin diacritics. Since each IAST character is
// a single codepoint (or NFC precomposed), a direct character map works.
// Note: IAST does NOT encode Preeti-style; it's for academic transliteration.

const IAST_MAP = {
  // Long vowels
  'ā': ['आ', 'ा'], 'Ā': ['आ', 'ा'],
  'ī': ['ई', 'ी'], 'Ī': ['ई', 'ी'],
  'ū': ['ऊ', 'ू'], 'Ū': ['ऊ', 'ू'],
  'ṛ': ['ऋ', 'ृ'], 'Ṛ': ['ऋ', 'ृ'],
  'ṝ': ['ॠ', 'ॄ'], 'Ṝ': ['ॠ', 'ॄ'],
  'ḷ': ['ऌ', 'ॢ'], 'Ḷ': ['ऌ', 'ॢ'],

  // Short vowels
  'a': ['अ', ''], 'A': null, // A in IAST context can be ambiguous, skip
  'i': ['इ', 'ि'],
  'u': ['उ', 'ु'],
  'e': ['ए', 'े'],
  'o': ['ओ', 'ो'],

  // Diphthongs (two-char but handled by preprocessing for IAST not needed — they're base chars)
  // ai / au handled below if present

  // Anusvara, visarga, chandrabindu
  'ṃ': ['ं', 'ं'], 'Ṃ': ['ं', 'ं'],
  'ḥ': ['ः', 'ः'], 'Ḥ': ['ः', 'ः'],
  'ṁ': ['ं', 'ं'], // alternative anusvara

  // Retroflex consonants
  'ṭ': ['ट', ''], 'Ṭ': ['ट', ''],
  'ḍ': ['ड', ''], 'Ḍ': ['ड', ''],
  'ṇ': ['ण', ''], 'Ṇ': ['ण', ''],

  // Palatal sibilant, retroflex sibilant
  'ś': ['श', ''], 'Ś': ['श', ''],
  'ṣ': ['ष', ''], 'Ṣ': ['ष', ''],

  // Velar nasal, palatal nasal
  'ṅ': ['ङ', ''], 'Ṅ': ['ङ', ''],
  'ñ': ['ञ', ''], 'Ñ': ['ञ', ''],

  // Regular consonants (IAST)
  'k': ['क', ''], 'g': ['ग', ''],
  'c': ['च', ''], 'j': ['ज', ''],
  't': ['त', ''], 'd': ['द', ''],
  'n': ['न', ''], 'p': ['प', ''],
  'b': ['ब', ''], 'm': ['म', ''],
  'y': ['य', ''], 'r': ['र', ''],
  'l': ['ल', ''], 'v': ['व', ''],
  's': ['स', ''], 'h': ['ह', ''],
};

// Export for module use
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { itransToUnicode, ITRANS_TOKENS, IAST_MAP };
}
