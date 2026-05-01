// migrations.go — Schema migration code for enrichment tables
//
// This migration code should be added to yogilib/main.go and integrated
// into the runMigrations() function. It creates the tables needed for
// the enrichment-watcher daemon to store its results.
//
// Tables created:
//   - embeddings: vector embeddings generated from document content
//   - summaries: AI-generated summaries (English + Devanagari Nepali)
//   - entities: named entities extracted from documents
//   - link_suggestions: suggested wiki links based on embedding similarity

// ============================================================================
// COPY THE FOLLOWING INTO yogilib/main.go (or your migration system)
// ============================================================================

/*

// migration5: Create enrichment tables (embeddings, summaries, entities, link_suggestions)
// Idempotent — safe to run on existing databases.
func migration5() error {
	stmts := []string{
		// Embeddings: vectors generated from document content using nomic-embed-text
		// vec: BLOB-encoded float32 array
		// dim: dimensionality of the embedding (e.g., 768 for nomic-embed-text)
		// body_hash: FNV-64 hash of the document body text, used to detect changes
		`CREATE TABLE IF NOT EXISTS embeddings (
			doc_id     INTEGER PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
			model      TEXT NOT NULL,
			vec        BLOB NOT NULL,
			dim        INTEGER NOT NULL,
			body_hash  TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,

		// Summaries: AI-generated summaries in two languages
		// summary: English or dominant language summary (4-6 sentences)
		// summary_np: Devanagari Nepali version (1-2 sentences)
		`CREATE TABLE IF NOT EXISTS summaries (
			doc_id     INTEGER PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
			model      TEXT NOT NULL,
			summary    TEXT NOT NULL,
			summary_np TEXT,
			body_hash  TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,

		// Entities: named entities extracted from document content
		// kind: person | place | work | date | other
		// value: entity name as it appears in text (preserves original script)
		// value_np: optional Devanagari translation/annotation
		// confidence: extraction confidence score (0.0-1.0)
		`CREATE TABLE IF NOT EXISTS entities (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id     INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			kind       TEXT NOT NULL,
			value      TEXT NOT NULL,
			value_np   TEXT,
			confidence REAL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_doc  ON entities(doc_id)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_kind ON entities(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_value ON entities(value)`,

		// Link suggestions: suggested wiki links based on embedding similarity
		// score: cosine similarity score (0.0-1.0)
		// status: pending | accepted | rejected (admin approval workflow)
		// anchor: suggested link text (default = target doc title)
		`CREATE TABLE IF NOT EXISTS link_suggestions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			from_doc_id  INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			to_doc_id    INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			anchor       TEXT NOT NULL,
			score        REAL,
			status       TEXT NOT NULL DEFAULT 'pending',
			created_at   TEXT NOT NULL,
			UNIQUE(from_doc_id, to_doc_id, anchor)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_link_suggestions_from ON link_suggestions(from_doc_id)`,
		`CREATE INDEX IF NOT EXISTS idx_link_suggestions_to   ON link_suggestions(to_doc_id)`,
		`CREATE INDEX IF NOT EXISTS idx_link_suggestions_status ON link_suggestions(status)`,
	}

	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("migration5: %w", err)
		}
	}
	return nil
}

// In runMigrations(), add this block after migration4:

	if version < 5 {
		if err := migration5(); err != nil {
			return fmt.Errorf("migration5: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 5"); err != nil {
			return err
		}
		log.Println("migration5: applied")
	}

*/

package main

// This file is documentation. The actual migration code should be
// integrated into yogilib/main.go as shown in the comment above.
