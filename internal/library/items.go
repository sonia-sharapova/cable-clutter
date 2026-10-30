package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"cable-clutter/internal/catalog"
)

// Item is one physical cable or adapter the user owns.
type Item struct {
	ID        string
	Kind      catalog.Kind
	PartKey   string
	Nickname  string
	Color     string
	Location  string
	Notes     string
	Dedicated string // nickname of the device it belongs to, if any
	CreatedAt int64
}

// ShortID is the handle shown in listings: the last 6 hex digits of the
// UUIDv7, which are random (the leading digits are a timestamp).
func (it *Item) ShortID() string {
	return strings.ReplaceAll(it.ID, "-", "")[26:]
}

// Status is derived from links and active systems; it is never stored.
type Status int

const (
	Free     Status = iota
	Reserved        // linked only in inactive systems
	InUse           // linked in an active system
)

func (s Status) String() string {
	return [...]string{"free", "reserved", "in use"}[s]
}

// Inventory is one kind of item (cables or adapters) loaded into hashmaps.
type Inventory struct {
	Items  map[string]*Item   // id -> item
	Owned  map[string][]*Item // part key -> items, oldest first
	Status map[string]Status  // id -> status
	Order  []*Item            // all items, oldest first
}

func table(kind catalog.Kind) string {
	if kind == catalog.KindAdapter {
		return "adapter_items"
	}
	return "cable_items"
}

func now() int64 { return time.Now().UnixMilli() }

// logChange appends to the sync log inside the caller's transaction.
func logChange(tx *sql.Tx, tbl, id, op string, at int64) error {
	_, err := tx.Exec(`INSERT INTO changes (tbl, row_id, op, at) VALUES (?, ?, ?, ?)`, tbl, id, op, at)
	return err
}

// AddItems inserts qty copies of it in one transaction and returns them.
// If it.Dedicated names a device that doesn't exist yet, the device is created.
func (db *DB) AddItems(it Item, qty int) ([]*Item, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	at := now()
	var deviceID sql.NullString
	if it.Dedicated != "" {
		id, err := ensureDevice(tx, it.Dedicated, at)
		if err != nil {
			return nil, err
		}
		deviceID = sql.NullString{String: id, Valid: true}
	}
	tbl := table(it.Kind)
	var out []*Item
	for range qty {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		n := it
		n.ID, n.CreatedAt = id.String(), at
		_, err = tx.Exec(`INSERT INTO `+tbl+` (id, part_key, nickname, color, location, notes, dedicated_device, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.PartKey, n.Nickname, n.Color, n.Location, n.Notes, deviceID, at, at)
		if err != nil {
			return nil, err
		}
		if err := logChange(tx, tbl, n.ID, "insert", at); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, tx.Commit()
}

// ensureDevice returns the id of the live device with this nickname
// (case-insensitive), creating a placeholder if there is none. Its catalog
// model is filled in once device models exist.
func ensureDevice(tx *sql.Tx, nickname string, at int64) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM devices WHERE deleted = 0 AND lower(nickname) = lower(?)`, nickname).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	u, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	id = u.String()
	if _, err := tx.Exec(`INSERT INTO devices (id, model, nickname, created_at, updated_at) VALUES (?, '', ?, ?, ?)`, id, nickname, at, at); err != nil {
		return "", err
	}
	return id, logChange(tx, "devices", id, "insert", at)
}

// RemoveItem soft-deletes an item (the tombstone syncs later).
func (db *DB) RemoveItem(kind catalog.Kind, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	tbl := table(kind)
	res, err := tx.Exec(`UPDATE `+tbl+` SET deleted = 1, updated_at = ?, rev = rev + 1 WHERE id = ? AND deleted = 0`, at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no %s with id %s", kind, id)
	}
	if err := logChange(tx, tbl, id, "delete", at); err != nil {
		return err
	}
	return tx.Commit()
}

// LoadInventory reads every live item of one kind and derives its status.
func (db *DB) LoadInventory(kind catalog.Kind) (*Inventory, error) {
	tbl := table(kind)
	rows, err := db.Query(`SELECT i.id, i.part_key, i.nickname, i.color, i.location, i.notes, COALESCE(d.nickname, ''), i.created_at
		FROM ` + tbl + ` i LEFT JOIN devices d ON d.id = i.dedicated_device
		WHERE i.deleted = 0 ORDER BY i.id`) // UUIDv7 ids sort by creation time
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inv := &Inventory{Items: map[string]*Item{}, Owned: map[string][]*Item{}, Status: map[string]Status{}}
	for rows.Next() {
		it := &Item{Kind: kind}
		if err := rows.Scan(&it.ID, &it.PartKey, &it.Nickname, &it.Color, &it.Location, &it.Notes, &it.Dedicated, &it.CreatedAt); err != nil {
			return nil, err
		}
		inv.Items[it.ID] = it
		inv.Owned[it.PartKey] = append(inv.Owned[it.PartKey], it)
		inv.Order = append(inv.Order, it)
		inv.Status[it.ID] = Free
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// An item linked in any active system is in use; linked only in inactive
	// systems, it is reserved.
	links, err := db.Query(`SELECT l.preferred_item, MAX(s.active) FROM links l
		JOIN systems s ON s.id = l.system_id AND s.deleted = 0
		WHERE l.deleted = 0 AND l.preferred_item IS NOT NULL GROUP BY l.preferred_item`)
	if err != nil {
		return nil, err
	}
	defer links.Close()
	for links.Next() {
		var id string
		var active bool
		if err := links.Scan(&id, &active); err != nil {
			return nil, err
		}
		if _, ok := inv.Items[id]; !ok {
			continue // a link to the other kind of item
		}
		inv.Status[id] = Reserved
		if active {
			inv.Status[id] = InUse
		}
	}
	return inv, links.Err()
}

// Lookup finds items by short id, full id or nickname (case-insensitive).
func (inv *Inventory) Lookup(ref string) []*Item {
	if it, ok := inv.Items[ref]; ok {
		return []*Item{it}
	}
	ref = strings.ToLower(strings.TrimPrefix(ref, "#"))
	var out []*Item
	for _, it := range inv.Order {
		if it.ShortID() == ref || (it.Nickname != "" && strings.ToLower(it.Nickname) == ref) {
			out = append(out, it)
		}
	}
	return out
}
