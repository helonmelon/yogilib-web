/**
 * preeti-unicode.js
 * Converts Preeti (ASCII-based Nepali font encoding) to Unicode Devanagari.
 * Preeti maps standard ASCII characters to Nepali glyphs visually;
 * this map reverses that back to proper Unicode code points.
 */

const PREETI_TO_UNICODE_MAP = {
  // Vowels / matras
  'A': 'ा', 'a': 'ा',   // aa matra (sometimes)
  'O': 'ो', 'o': 'ो',
  'P': 'प', 'p': 'प',
  'Q': 'ृ', 'q': 'श्र',
  'R': 'र', 'r': 'र',
  'S': 'स', 's': 'स',
  'T': 'त', 't': 'त',
  'U': 'ु', 'u': 'ु',
  'V': 'व', 'v': 'व',
  'W': 'ध', 'w': 'ध',
  'X': 'ं', 'x': 'ं',
  'Y': 'य', 'y': 'य',
  'Z': 'त्र', 'z': 'ज्ञ',
  'B': 'ब', 'b': 'ब',
  'C': 'च', 'c': 'च',
  'D': 'ड', 'd': 'द',
  'E': 'े', 'e': 'े',
  'F': 'फ', 'f': 'फ',
  'G': 'ग', 'g': 'ग',
  'H': 'ह', 'h': 'ह',
  'I': 'ि', 'i': 'ि',
  'J': 'ज', 'j': 'ज',
  'K': 'क', 'k': 'क',
  'L': 'ल', 'l': 'ल',
  'M': 'म', 'm': 'म',
  'N': 'ण', 'n': 'न',

  // Numbers (Nepali digits)
  '0': '०', '1': '१', '2': '२', '3': '३', '4': '४',
  '5': '५', '6': '६', '7': '७', '8': '८', '9': '९',

  // Punctuation / special
  '|': '।',   // daṇḍa (full stop in Nepali)
  '\\': '्',  // halant / virama
  '^': 'ँ',   // chandrabindu
  '*': 'ू',   // uu matra
  '+': 'ौ',
  '=': 'ृ',
  '[': 'ि',
  ']': 'ी',
  '{': 'ो',
  '}': 'ौ',
  ';': 'ः',
  "'": 'ं',
  '"': 'ँ',
  '<': ',',
  '>': '।',
  '?': '?',
  '!': '!',
  '@': 'ॄ',
  '#': 'ृ',
  '$': 'ं',
  '%': '%',
  '&': 'ु',
  '(': '(',
  ')': ')',
  '-': '-',
  '_': '_',
  ',': ',',
  '.': '.',
  '/': '÷',
  '`': '`',
  '~': '~',
};

// More precise Preeti map based on actual font encoding
const PREETI_MAP_PRECISE = {
  // Consonants (based on Preeti font standard)
  'k': 'क', 'K': 'ख', 'g': 'ग', 'G': 'घ', '^': 'ङ',
  'r': 'च', 'R': 'छ', 'j': 'ज', 'J': 'झ',
  'b': 'ट', 'B': 'ठ', 'f': 'ड', 'F': 'ढ', 'N': 'ण',
  't': 'त', 'T': 'थ', 'd': 'द', 'D': 'ध', 'n': 'न',
  'p': 'प', 'P': 'फ', 'j': 'ब', // Note: conflict — handle ordering
  'm': 'म', 'o': 'य', 'y': 'र', 'l': 'ल', 'v': 'व',
  'z': 'श', 'S': 'ष', 's': 'स', 'x': 'ह',
  'L': 'ल', 'M': 'म',

  // Vowels (standalone)
  'a': 'अ', 'A': 'आ', 'O': 'ई', 'e': 'उ', 'E': 'ऊ',

  // Matras (vowel signs)
  'f': 'ा',  // aa-matra
  'L': 'े',  // e-matra
  'u': 'ु',  // u-matra
  'U': 'ू',  // uu-matra
  'I': 'ि',  // i-matra (before consonant display)
  'i': 'ी',  // ii-matra
  'C': 'ो',  // o-matra
  'H': 'ौ',  // au-matra

  // Numbers
  '0': '०', '1': '१', '2': '२', '3': '३', '4': '४',
  '5': '५', '6': '६', '7': '७', '8': '८', '9': '९',

  // Punctuation
  '|': '।', '\\': '्',
};

/**
 * The authoritative Preeti→Unicode table
 * Source: widely used Preeti encoding standard
 */
const PREETI = {
  '!': 'ऽ', '@': 'ु', '#': 'ू', '$': 'ः', '%': '५', '^': 'ँ', '&': '७', '*': '८', '(': '९', ')': ')',
  'q': 'ौ', 'w': 'ै', 'e': 'ा', 'r': 'र', 't': 'त', 'y': 'य', 'u': 'ु', 'i': 'ि', 'o': 'ो', 'p': 'प',
  'Q': 'ौ', 'W': 'ृ', 'E': 'ए', 'R': 'र्‍', 'T': 'ट', 'Y': 'य', 'U': 'ू', 'I': 'ी', 'O': 'ओ', 'P': 'फ',
  'a': 'ा', 's': 'स', 'd': 'द', 'f': 'फ', 'g': 'ग', 'h': 'ह', 'j': 'ज', 'k': 'क', 'l': 'ल',
  'A': 'ा', 'S': 'ष', 'D': 'ध', 'F': 'ब', 'G': 'घ', 'H': 'ख', 'J': 'झ', 'K': 'ष', 'L': 'श',
  'z': 'ज्ञ', 'x': 'ं', 'c': 'च', 'v': 'व', 'b': 'ब', 'n': 'न', 'm': 'म',
  'Z': 'त्र', 'X': 'अं', 'C': 'छ', 'V': 'व', 'B': 'भ', 'N': 'ण', 'M': 'म',
  '1': '१', '2': '२', '3': '३', '4': '४', '5': '५', '6': '६', '7': '७', '8': '८', '9': '९', '0': '०',
  ' ': ' ', '.': '।', ',': ',', ';': 'ः', "'": 'ं', '"': 'ँ', ':': ':', '-': '-', '_': '_',
  '/': '/', '`': '`', '~': '~', '+': '+', '=': '=', '|': '।', '\\': '्',
  '[': 'ृ', ']': '्', '{': 'े', '}': 'ै', '(': 'ो', ')': 'ौ',
  '<': 'ि', '>': 'ी',
};

/**
 * Convert a Preeti-encoded string to Unicode Devanagari.
 * @param {string} input  Preeti-encoded text
 * @returns {string}      Unicode Devanagari text
 */
function preetiToUnicode(input) {
  if (!input) return '';
  let output = '';
  for (let i = 0; i < input.length; i++) {
    const ch = input[i];
    output += PREETI[ch] !== undefined ? PREETI[ch] : ch;
  }
  return output;
}

// Export for module use
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { preetiToUnicode, PREETI };
}
