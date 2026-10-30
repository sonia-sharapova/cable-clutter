package library

import "fmt"

// Wish is a part type on the shopping list.
type Wish struct {
	ID      string
	PartKey string
	Qty     int
	System  string // name of the system it's for, if any
	Notes   string
	Done    bool
}

// Wishes lists the wishlist, oldest first; done items only if asked.
func (db *DB) Wishes(withDone bool) ([]*Wish, error) {
	rows, err := db.Query(`SELECT w.id, w.part_key, w.qty, COALESCE(s.name, ''), w.notes, w.done
		FROM wishlist w LEFT JOIN systems s ON s.id = w.system_id AND s.deleted = 0
		WHERE w.deleted = 0 AND (? OR w.done = 0) ORDER BY w.id`, withDone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Wish
	for rows.Next() {
		w := &Wish{}
		if err := rows.Scan(&w.ID, &w.PartKey, &w.Qty, &w.System, &w.Notes, &w.Done); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Wanted reports whether an open wish for this part type exists.
func (db *DB) Wanted(partKey string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM wishlist WHERE deleted = 0 AND done = 0 AND part_key = ?`, partKey).Scan(&n)
	return n > 0, err
}

// MarkWishDone ticks a wish off and logs its items as owned, in one
// transaction, so the inventory and the wishlist never disagree.
func (db *DB) MarkWishDone(w *Wish, item Item) ([]*Item, error) {
	if w.Done {
		return nil, fmt.Errorf("already done")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	at := now()
	if _, err := tx.Exec(`UPDATE wishlist SET done = 1, updated_at = ?, rev = rev + 1 WHERE id = ?`, at, w.ID); err != nil {
		return nil, err
	}
	if err := logChange(tx, "wishlist", w.ID, "update", at); err != nil {
		return nil, err
	}
	item.PartKey = w.PartKey
	added, err := addItems(tx, item, w.Qty, at)
	if err != nil {
		return nil, err
	}
	return added, tx.Commit()
}

// RemoveWish deletes a wish.
func (db *DB) RemoveWish(id string) error { return db.softDelete("wishlist", id) }
