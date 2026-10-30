package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// System is a named setup (TV, retro corner) built from links.
type System struct {
	ID     string
	Name   string
	Notes  string
	Active bool
}

// Link joins two device ports with one cable or adapter type. Preferred
// pins a specific item; Assigned is the item in use while the system is up.
// Devices are nicknames; any end may be left open.
type Link struct {
	ID         string
	SystemID   string
	PartKey    string
	Preferred  string
	Assigned   string
	FromDevice string
	FromPort   string
	ToDevice   string
	ToPort     string
}

func newID() (string, error) {
	u, err := uuid.NewV7()
	return u.String(), err
}

func (db *DB) NewSystem(name, notes string) (*System, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("a system needs a name")
	}
	if _, err := db.SystemByName(name); err == nil {
		return nil, fmt.Errorf("there is already a system called %q", name)
	}
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
	if _, err := tx.Exec(`INSERT INTO systems (id, name, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, name, notes, at, at); err != nil {
		return nil, err
	}
	if err := logChange(tx, "systems", id, "insert", at); err != nil {
		return nil, err
	}
	return &System{ID: id, Name: name, Notes: notes}, tx.Commit()
}

// Systems lists live systems, oldest first.
func (db *DB) Systems() ([]*System, error) {
	rows, err := db.Query(`SELECT id, name, notes, active FROM systems WHERE deleted = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*System
	for rows.Next() {
		s := &System{}
		if err := rows.Scan(&s.ID, &s.Name, &s.Notes, &s.Active); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SystemByName finds a live system, ignoring case.
func (db *DB) SystemByName(name string) (*System, error) {
	s := &System{}
	err := db.QueryRow(`SELECT id, name, notes, active FROM systems WHERE deleted = 0 AND lower(name) = lower(?)`, strings.TrimSpace(name)).
		Scan(&s.ID, &s.Name, &s.Notes, &s.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no system called %q; see `cbl system ls`", name)
	}
	return s, err
}

// Links returns every live link, grouped by system id, each group in the
// order the links were added.
func (db *DB) Links() (map[string][]*Link, error) {
	rows, err := db.Query(`SELECT l.id, l.system_id, l.part_key, COALESCE(l.preferred_item, ''), COALESCE(l.assigned_item, ''),
			COALESCE(fd.nickname, ''), l.from_port, COALESCE(td.nickname, ''), l.to_port
		FROM links l
		LEFT JOIN devices fd ON fd.id = l.from_device
		LEFT JOIN devices td ON td.id = l.to_device
		WHERE l.deleted = 0 ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]*Link{}
	for rows.Next() {
		l := &Link{}
		if err := rows.Scan(&l.ID, &l.SystemID, &l.PartKey, &l.Preferred, &l.Assigned, &l.FromDevice, &l.FromPort, &l.ToDevice, &l.ToPort); err != nil {
			return nil, err
		}
		out[l.SystemID] = append(out[l.SystemID], l)
	}
	return out, rows.Err()
}

// AddLink adds a link to a system. Device fields are nicknames of existing
// devices, or "".
func (db *DB) AddLink(l Link) (*Link, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	deviceID := func(nick string) (sql.NullString, error) {
		if nick == "" {
			return sql.NullString{}, nil
		}
		var id string
		err := tx.QueryRow(`SELECT id FROM devices WHERE deleted = 0 AND lower(nickname) = lower(?)`, nick).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return sql.NullString{}, fmt.Errorf("no device called %q; see `cbl device ls`", nick)
		}
		return sql.NullString{String: id, Valid: true}, err
	}
	from, err := deviceID(l.FromDevice)
	if err != nil {
		return nil, err
	}
	to, err := deviceID(l.ToDevice)
	if err != nil {
		return nil, err
	}
	if l.ID, err = newID(); err != nil {
		return nil, err
	}
	pref := sql.NullString{String: l.Preferred, Valid: l.Preferred != ""}
	at := now()
	_, err = tx.Exec(`INSERT INTO links (id, system_id, part_key, preferred_item, from_device, from_port, to_device, to_port, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, l.ID, l.SystemID, l.PartKey, pref, from, l.FromPort, to, l.ToPort, at, at)
	if err != nil {
		return nil, err
	}
	if err := logChange(tx, "links", l.ID, "insert", at); err != nil {
		return nil, err
	}
	return &l, tx.Commit()
}

// RemoveLink deletes a link from a system that is down.
func (db *DB) RemoveLink(id string) error {
	return db.softDelete("links", id)
}

// RemoveSystem deletes a system that is down, with its links.
func (db *DB) RemoveSystem(s *System) error {
	if s.Active {
		return fmt.Errorf("%q is up; take it down first", s.Name)
	}
	links, err := db.Links()
	if err != nil {
		return err
	}
	for _, l := range links[s.ID] {
		if err := db.softDelete("links", l.ID); err != nil {
			return err
		}
	}
	return db.softDelete("systems", s.ID)
}

func (db *DB) softDelete(tbl, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	res, err := tx.Exec(`UPDATE `+tbl+` SET deleted = 1, updated_at = ?, rev = rev + 1 WHERE id = ? AND deleted = 0`, at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no %s row %s", tbl, id)
	}
	if err := logChange(tx, tbl, id, "delete", at); err != nil {
		return err
	}
	return tx.Commit()
}

// BringUp marks a system active and records which item each link uses
// (link id -> item id), in one transaction.
func (db *DB) BringUp(systemID string, assigned map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	for linkID, itemID := range assigned {
		if _, err := tx.Exec(`UPDATE links SET assigned_item = ?, updated_at = ?, rev = rev + 1 WHERE id = ?`, itemID, at, linkID); err != nil {
			return err
		}
		if err := logChange(tx, "links", linkID, "update", at); err != nil {
			return err
		}
	}
	if err := setActive(tx, systemID, true, at); err != nil {
		return err
	}
	return tx.Commit()
}

// TakeDown marks a system inactive and frees the items its links used.
func (db *DB) TakeDown(systemID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err := tx.Exec(`UPDATE links SET assigned_item = NULL, updated_at = ?, rev = rev + 1
		WHERE system_id = ? AND deleted = 0 AND assigned_item IS NOT NULL`, at, systemID); err != nil {
		return err
	}
	if err := setActive(tx, systemID, false, at); err != nil {
		return err
	}
	return tx.Commit()
}

func setActive(tx *sql.Tx, systemID string, active bool, at int64) error {
	if _, err := tx.Exec(`UPDATE systems SET active = ?, updated_at = ?, rev = rev + 1 WHERE id = ?`, active, at, systemID); err != nil {
		return err
	}
	return logChange(tx, "systems", systemID, "update", at)
}

// AddWish puts a part type on the shopping list, optionally for a system.
func (db *DB) AddWish(partKey string, qty int, systemID, notes string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := newID()
	if err != nil {
		return err
	}
	sys := sql.NullString{String: systemID, Valid: systemID != ""}
	at := now()
	if _, err := tx.Exec(`INSERT INTO wishlist (id, part_key, qty, system_id, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, partKey, qty, sys, notes, at, at); err != nil {
		return err
	}
	if err := logChange(tx, "wishlist", id, "insert", at); err != nil {
		return err
	}
	return tx.Commit()
}
