package library

// CustomEntry is a catalog entry you added yourself: kind is cable, adapter
// or device, id the part key or device model id, and body its JSON.
type CustomEntry struct {
	Kind string
	ID   string
	Body []byte
}

// CustomCatalog lists your own catalog entries, oldest first.
func (db *DB) CustomCatalog() ([]CustomEntry, error) {
	rows, err := db.Query(`SELECT kind, id, body FROM custom_catalog WHERE deleted = 0 ORDER BY created_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomEntry
	for rows.Next() {
		var e CustomEntry
		if err := rows.Scan(&e.Kind, &e.ID, &e.Body); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddCustom stores a catalog entry, reviving one of the same kind and id if
// it was deleted.
func (db *DB) AddCustom(e CustomEntry) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err := tx.Exec(`INSERT INTO custom_catalog (kind, id, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (kind, id) DO UPDATE SET body = excluded.body, deleted = 0, updated_at = excluded.updated_at, rev = rev + 1`,
		e.Kind, e.ID, string(e.Body), at, at); err != nil {
		return err
	}
	if err := logChange(tx, "custom_catalog", e.Kind+":"+e.ID, "insert", at); err != nil {
		return err
	}
	return tx.Commit()
}
