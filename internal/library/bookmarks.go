package library

// Bookmark is a saved route between two devices.
type Bookmark struct {
	ID   string
	Name string // optional; the page shows "From → To" without one
	From string
	To   string
}

// Bookmarks lists the saved routes, oldest first.
func (db *DB) Bookmarks() ([]*Bookmark, error) {
	rows, err := db.Query(`SELECT id, name, from_device, to_device FROM bookmarks WHERE deleted = 0 ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Bookmark
	for rows.Next() {
		b := &Bookmark{}
		if err := rows.Scan(&b.ID, &b.Name, &b.From, &b.To); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AddBookmark saves a route and returns it with its id.
func (db *DB) AddBookmark(name, from, to string) (*Bookmark, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, err := newID()
	if err != nil {
		return nil, err
	}
	at := now()
	if _, err := tx.Exec(`INSERT INTO bookmarks (id, name, from_device, to_device, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, from, to, at, at); err != nil {
		return nil, err
	}
	if err := logChange(tx, "bookmarks", id, "insert", at); err != nil {
		return nil, err
	}
	return &Bookmark{ID: id, Name: name, From: from, To: to}, tx.Commit()
}

// RemoveBookmark deletes a saved route.
func (db *DB) RemoveBookmark(id string) error { return db.softDelete("bookmarks", id) }
