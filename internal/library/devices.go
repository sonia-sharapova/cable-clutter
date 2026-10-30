package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Device is one physical device the user owns. Model is a catalog device
// model ID, or "" for a placeholder created by `cbl add --for` that hasn't
// been given a model yet.
type Device struct {
	ID       string
	Model    string
	Nickname string
	Notes    string
	Owner    string // whose it is, if you said
}

func (d *Device) ShortID() string { return strings.ReplaceAll(d.ID, "-", "")[26:] }

// AddDevice records a device. If a placeholder with this nickname exists, it
// gets the model instead of a second device being created; created reports
// which happened.
func (db *DB) AddDevice(model, nickname, notes string) (d *Device, created bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	at := now()

	var id, existing string
	err = tx.QueryRow(`SELECT id, model FROM devices WHERE deleted = 0 AND lower(nickname) = lower(?)`, nickname).Scan(&id, &existing)
	switch {
	case err == nil && existing != "":
		return nil, false, fmt.Errorf("you already have a device called %q", nickname)
	case err == nil:
		_, err = tx.Exec(`UPDATE devices SET model = ?, notes = CASE WHEN ? = '' THEN notes ELSE ? END, updated_at = ?, rev = rev + 1 WHERE id = ?`,
			model, notes, notes, at, id)
		if err == nil {
			err = logChange(tx, "devices", id, "update", at)
		}
	case errors.Is(err, sql.ErrNoRows):
		u, uerr := uuid.NewV7()
		if uerr != nil {
			return nil, false, uerr
		}
		id, created = u.String(), true
		_, err = tx.Exec(`INSERT INTO devices (id, model, nickname, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, model, nickname, notes, at, at)
		if err == nil {
			err = logChange(tx, "devices", id, "insert", at)
		}
	}
	if err != nil {
		return nil, false, err
	}
	return &Device{ID: id, Model: model, Nickname: nickname, Notes: notes}, created, tx.Commit()
}

// Devices lists live devices, oldest first.
func (db *DB) Devices() ([]*Device, error) {
	rows, err := db.Query(`SELECT id, model, nickname, notes, owner FROM devices WHERE deleted = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d := &Device{}
		if err := rows.Scan(&d.ID, &d.Model, &d.Nickname, &d.Notes, &d.Owner); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetOwner records whose a device is ("" for nobody in particular).
func (db *DB) SetOwner(id, owner string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	res, err := tx.Exec(`UPDATE devices SET owner = ?, updated_at = ?, rev = rev + 1 WHERE id = ? AND deleted = 0`, strings.TrimSpace(owner), at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no device %s", id)
	}
	if err := logChange(tx, "devices", id, "update", at); err != nil {
		return err
	}
	return tx.Commit()
}

// FindDevices matches devices by short id or nickname (case-insensitive).
func FindDevices(all []*Device, ref string) []*Device {
	ref = strings.ToLower(strings.TrimPrefix(ref, "#"))
	var out []*Device
	for _, d := range all {
		if d.ShortID() == ref || strings.ToLower(d.Nickname) == ref {
			out = append(out, d)
		}
	}
	return out
}

// RemoveDevice soft-deletes a device. Cables and adapters dedicated to it
// stay in the inventory, no longer dedicated.
func (db *DB) RemoveDevice(id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	res, err := tx.Exec(`UPDATE devices SET deleted = 1, updated_at = ?, rev = rev + 1 WHERE id = ? AND deleted = 0`, at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no device with id %s", id)
	}
	for _, tbl := range []string{"cable_items", "adapter_items"} {
		rows, err := tx.Query(`SELECT id FROM `+tbl+` WHERE dedicated_device = ? AND deleted = 0`, id)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var item string
			rows.Scan(&item)
			ids = append(ids, item)
		}
		rows.Close()
		for _, item := range ids {
			if _, err := tx.Exec(`UPDATE `+tbl+` SET dedicated_device = NULL, updated_at = ?, rev = rev + 1 WHERE id = ?`, at, item); err != nil {
				return err
			}
			if err := logChange(tx, tbl, item, "update", at); err != nil {
				return err
			}
		}
	}
	if err := logChange(tx, "devices", id, "delete", at); err != nil {
		return err
	}
	return tx.Commit()
}
