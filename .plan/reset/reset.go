// Reset utility: wipes test docs (id != 1), clears all derived data
// (revisions, links, embeddings, summaries, entities, link_suggestions),
// then rewrites doc/1 with the canonical Treaty of Sugowlee text.
//
// Run from the repo root with the server STOPPED:
//   cd ~/.openclaw/workspace/yogilib/yogilib-web
//   go run .plan/reset/reset.go
//
// Then restart the server.

//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const treatyTitle = "The Treaty of Sugowlee"
const treatyTitleNP = "सुगौलीको सन्धि"
const treatyDescription = "Treaty of Peace between the Honourable East India Company and Maha Rajah Bikram Sah, Rajah of Nipal — signed 2 December 1815, ratified 4 March 1816."

const treatyBodyHTML = `<p><strong>The Treaty of Sugowlee</strong><br>
Signed on 2 December 1815 and<br>
Ratified by 4 March 1816</p>

<p>Treaty of Peace between the Honourable East India Company and Maha Rajah Bikram Sah, Rajah of Nipal, settled between Lieutenant-Colonel Bradshaw on the part of the Honourable Company, in virtue of the full powers vested in him by his Excellency the Right Honourable Francis, Earl of Moira, Knight of the Most Noble Order of the Garter, one of His Majesty's Most Honourable Privy Council, appointed by the Court of Directors of the said Honourable Company to direct and control all the affairs in the East Indies, and by Sree Gooroo Gujraj Misser and Chunder Seekur Opedeea on the part of Maha Rajah Girmaun Jode Bikram Shah Bauder, Shumsheer Jung, in virtue of the powers to that effect vested in them by the said Rajah of Nipal — 2nd December 1815.</p>

<p>WHEREAS war has arisen between the Honourable East India Company and the Rajah of Nipal, and</p>

<p>WHEREAS the parties are mutually disposed to restore the relations of peace and amity which, previously to the occurrence of the late differences, had long subsisted between the two States, the following terms of peace have been agreed upon:</p>

<h2>Article 1st</h2>
<p>There shall be perpetual peace and friendship between the Honourable East Company and the Rajah of Nipal.</p>

<h2>Article 2nd</h2>
<p>The Rajah of Nipal renounces all claim to the lands which were the subject of discussion between the two States before the war; and acknowledges the right of the Honourable Company to the sovereignty of those lands.</p>

<h2>Article 3rd</h2>
<p>The Rajah of Nipal hereby cedes to the Honourable East India Company in perpetuity all the under-mentioned territories, viz. —</p>
<ol>
  <li><strong>Firstly</strong> &mdash; The whole of the low lands between the Rivers Kali and Rapti.</li>
  <li><strong>Secondly</strong> &mdash; The whole of the low lands (with the exception of Bootwul Khass) lying between the Rapti and the Gunduck.</li>
  <li><strong>Thirdly</strong> &mdash; The whole of the low lands between the Gunduck and Coosah, in which the authority of the British government has been introduced, or is in actual course of introduction.</li>
  <li><strong>Fourthly</strong> &mdash; All the low lands between the Rivers Mitchee and the Teestah.</li>
  <li><strong>Fifthly</strong> &mdash; All the territories within the hills eastward of the River Mitchee including the fort and lands of Nagree and the Pass of Nagarcote leading from Morung into the hills, together with the territory lying between that Pass and Nagree. The aforesaid territory shall be evacuated by the Gurkah troops within forty days from this date.</li>
</ol>

<h2>Article 4th</h2>
<p>With a view to indemnify the Chiefs and Barahdars of the State of Nipal, whose interests will suffer by the alienation of the lands ceded by the foregoing Article, the British Government agrees to settle pensions to the aggregate amount of two lakhs of rupees per annum on such Chiefs as may be selected by the Rajah of Nipal, and in the proportions which the Rajah may fix. As soon as the selection is made, Sunnuds shall be granted under the seal and signature of the Governor-General for the pensions respectively.</p>

<h2>Article 5th</h2>
<p>The Rajah of Nipal renounces for himself, his heirs, and successors, all claim to or connexion with the countries lying to the west of the River Kali and engages never to have any concern with those countries or the inhabitants thereof.</p>

<h2>Article 6th</h2>
<p>The Rajah of Nipal engages never to molest or disturb the Rajah of Sikkim in the possession of his territories; but agrees, if any differences shall arise between the State of Nipal and the Rajah of Sikkim or the subjects of either, that such differences shall be referred to the arbitration of the British Government by whose award the Rajah of Nipal engages to abide.</p>

<h2>Article 7th</h2>
<p>The Rajah of Nipal hereby engages never to take or retain in his service any British subject, nor the subject of any European and American State, without the consent of the British Government.</p>

<h2>Article 8th</h2>
<p>In order to secure and improve the relations of amity and peace hereby established between the two States, it is agreed that accredited Ministers from each shall reside at the Court of the other.</p>

<h2>Article 9th</h2>
<p>This treaty, consisting of nine Articles, shall be ratified by the Rajah of Nipal within fifteen days from this date, and the ratification shall be delivered to Lieut.-Colonel Bradshaw, who engages to obtain and deliver to the Rajah the ratification of the Governor-General within twenty days, or sooner, if practicable.</p>

<p style="margin-top:2rem"><em>DONE at Segowlee, on the 2nd day of December 1815.</em></p>

<p><strong>PARIS BRADSHAW, LT.-COL., P.A.</strong></p>

<p style="margin-top:1.5rem"><em>Received this treaty from Chunder Seekur Opedeea, Agent on the part of the Rajah of Nipal, in the valley of Muckwaunpoor, at half-past two o'clock p.m. on the 4th of March 1816, and delivered to him the Counterpart Treaty on behalf of the British Government.</em></p>

<p><strong>D. D. OCHTERLONY,</strong><br>
Agent, Governor-General</p>`

// minimal stripHTML
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteByte(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	// collapse whitespace
	out := b.String()
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	return strings.TrimSpace(out)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prev := true
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prev = false
		case r == ' ' || r == '-' || r == '_':
			if !prev {
				b.WriteByte('-')
				prev = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "yogilib.db"
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	bodyText := stripHTML(treatyBodyHTML)
	now := time.Now().UTC().Format(time.RFC3339)
	createdAt := time.Now().Format("2 Jan 2006")

	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()

	// 1. Clear ALL referencing tables first (revisions doesn't have CASCADE).
	if _, err := tx.Exec(`DELETE FROM revisions`); err != nil {
		log.Fatal("clear revisions:", err)
	}
	if _, err := tx.Exec(`DELETE FROM links`); err != nil {
		log.Fatal("clear links:", err)
	}
	tx.Exec(`DELETE FROM embeddings`)
	tx.Exec(`DELETE FROM summaries`)
	tx.Exec(`DELETE FROM entities`)
	tx.Exec(`DELETE FROM link_suggestions`)

	// 2. Now safe to delete extra docs.
	if _, err := tx.Exec(`DELETE FROM documents WHERE id != 1`); err != nil {
		log.Fatal("delete docs:", err)
	}

	// 4. Upsert doc/1 with the canonical Treaty content
	slug := slugify(treatyTitle)
	res, err := tx.Exec(`
		INSERT INTO documents (id, title, title_np, slug, category, description,
			body_html, body_text, lang, script,
			orig_author, orig_year, orig_month, orig_day,
			file_path, uploaded_by, created_at)
		VALUES (1, ?, ?, ?, 'कागजात', ?,
			?, ?, 'en', 'Latin',
			'East India Company / Kingdom of Nepal', '1815', '12', '2',
			'', NULL, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title,
			title_np=excluded.title_np,
			slug=excluded.slug,
			category=excluded.category,
			description=excluded.description,
			body_html=excluded.body_html,
			body_text=excluded.body_text,
			lang=excluded.lang,
			script=excluded.script,
			orig_author=excluded.orig_author,
			orig_year=excluded.orig_year,
			orig_month=excluded.orig_month,
			orig_day=excluded.orig_day
	`, treatyTitle, treatyTitleNP, slug, treatyDescription,
		treatyBodyHTML, bodyText, createdAt)
	if err != nil {
		log.Fatal("upsert doc:", err)
	}
	_ = res

	// 5. Initial revision row capturing this state
	if _, err := tx.Exec(`
		INSERT INTO revisions (doc_id, title, title_np, body_html, body_text, edited_by, edited_at, comment)
		VALUES (1, ?, ?, ?, ?, NULL, ?, 'reset to canonical treaty text')
	`, treatyTitle, treatyTitleNP, treatyBodyHTML, bodyText, now); err != nil {
		log.Fatal("revision insert:", err)
	}

	if err := tx.Commit(); err != nil {
		log.Fatal("commit:", err)
	}

	fmt.Println("RESET OK")
	fmt.Println("- doc/1 = ", treatyTitle)
	fmt.Println("- slug  = ", slug)
	fmt.Println("- body  = ", len(treatyBodyHTML), "chars HTML /", len(bodyText), "chars text")
	fmt.Println("- all other docs deleted; revisions/links/enrichment cleared")
}
