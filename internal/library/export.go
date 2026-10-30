package library

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ExportFormat names the export document type.
const ExportFormat = "cable-clutter-export"

// exportTables in foreign-key order: a table only references ones before it.
var exportTables = []string{"devices", "cable_items", "adapter_items", "systems", "links", "wishlist", "custom_catalog"}

// Export is a plain-JSON copy of every live row, keeping ids, so a backup
// restores exactly and a future sync can match rows.
type Export struct {
	Format     string                      `json:"format"`
	Schema     int                         `json:"schema"`
	Catalog    string                      `json:"catalog"`
	ExportedAt string                      `json:"exported_at"`
	Tables     map[string][]map[string]any `json:"tables"`
}

// Export reads every live row of every user table.
func (db *DB) Export(catalogVersion string) (*Export, error) {
	schema, err := db.SchemaVersion()
	if err != nil {
		return nil, err
	}
	e := &Export{Format: ExportFormat, Schema: schema, Catalog: catalogVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), Tables: map[string][]map[string]any{}}
	for _, tbl := range exportTables {
		rows, err := db.Query(`SELECT * FROM ` + tbl + ` WHERE deleted = 0 ORDER BY rowid`)
		if err != nil {
			return nil, err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		e.Tables[tbl] = []map[string]any{}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			row := map[string]any{}
			for i, c := range cols {
				if b, ok := vals[i].([]byte); ok {
					vals[i] = string(b)
				}
				row[c] = vals[i]
			}
			e.Tables[tbl] = append(e.Tables[tbl], row)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// ParseExport reads an export document, keeping integers exact.
func ParseExport(data []byte) (*Export, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var e Export
	if err := dec.Decode(&e); err != nil {
		return nil, fmt.Errorf("not a cable-clutter export: %w", err)
	}
	if e.Format != ExportFormat {
		return nil, fmt.Errorf("not a cable-clutter export (format %q)", e.Format)
	}
	for _, rows := range e.Tables {
		for _, row := range rows {
			for k, v := range row {
				if n, ok := v.(json.Number); ok {
					if i, err := n.Int64(); err == nil {
						row[k] = i
					} else {
						row[k] = n.String()
					}
				}
			}
		}
	}
	return &e, nil
}

// Import loads an export in one transaction. The library must be empty
// unless replace is set, which first clears it. Columns the export lacks
// take their defaults, so an export from an older schema still loads.
func (db *DB) Import(e *Export, replace bool) (map[string]int, error) {
	schema, err := db.SchemaVersion()
	if err != nil {
		return nil, err
	}
	if e.Schema > schema {
		return nil, fmt.Errorf("the export is from a newer cbl (schema v%d, this one is v%d); update cbl first", e.Schema, schema)
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if replace {
		for i := len(exportTables) - 1; i >= 0; i-- {
			if _, err := tx.Exec(`DELETE FROM ` + exportTables[i]); err != nil {
				return nil, err
			}
		}
	} else {
		for _, tbl := range exportTables {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM ` + tbl + ` WHERE deleted = 0`).Scan(&n); err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, errors.New("your library isn't empty; import with --replace to overwrite it (export it first to keep a copy)")
			}
		}
	}

	at := now()
	counts := map[string]int{}
	for _, tbl := range exportTables {
		cols, err := columns(tx, tbl)
		if err != nil {
			return nil, err
		}
		for _, row := range e.Tables[tbl] {
			var names, marks []string
			var vals []any
			for _, c := range cols {
				if v, ok := row[c]; ok {
					names, marks, vals = append(names, c), append(marks, "?"), append(vals, v)
				}
			}
			if _, err := tx.Exec(`INSERT INTO `+tbl+` (`+strings.Join(names, ", ")+`) VALUES (`+strings.Join(marks, ", ")+`)`, vals...); err != nil {
				return nil, fmt.Errorf("%s: %w", tbl, err)
			}
			id, _ := row["id"].(string)
			if err := logChange(tx, tbl, id, "insert", at); err != nil {
				return nil, err
			}
			counts[tbl]++
		}
	}
	return counts, tx.Commit()
}

// columns lists a table's columns as the current schema defines them.
func columns(tx *sql.Tx, tbl string) ([]string, error) {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, tbl)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
