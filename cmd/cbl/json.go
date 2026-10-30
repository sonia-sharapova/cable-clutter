package main

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

// jsonOut is the global --json flag: every command then prints one JSON
// document on stdout instead of text, and errors become {"error": "..."}.
var jsonOut bool

// errReported means the command already printed a JSON document describing
// its failure; main exits non-zero without printing anything more.
var errReported = errors.New("reported")

// textOut is where a command writes its text: stdout, or nowhere under --json.
func textOut(cmd *cobra.Command) io.Writer {
	if jsonOut {
		return io.Discard
	}
	return cmd.OutOrStdout()
}

func emit(cmd *cobra.Command, v any) error {
	return writeJSON(cmd.OutOrStdout(), v)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

type partView struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Ends    []string `json:"ends"`
	Signals []string `json:"signals"`
	Variant string   `json:"variant,omitempty"`
	Active  bool     `json:"active,omitempty"`
	Source  string   `json:"source,omitempty"`
	Volts   float64  `json:"volts,omitempty"`
	Amps    float64  `json:"amps,omitempty"`
	Note    string   `json:"note,omitempty"`
}

func viewPart(p *catalog.Part) partView {
	ends := make([]string, len(p.Ends))
	for i, e := range p.Ends {
		ends[i] = e.String()
	}
	return partView{Key: p.Key, Name: p.Name, Kind: string(p.Kind), Ends: ends, Signals: p.Signals, Variant: p.Variant,
		Active: p.Active, Source: p.Source, Volts: p.Volts, Amps: p.Amps, Note: p.Note}
}

func viewParts(ps []*catalog.Part) []partView {
	out := make([]partView, len(ps))
	for i, p := range ps {
		out[i] = viewPart(p)
	}
	return out
}

type itemView struct {
	ID       string `json:"id"`
	ShortID  string `json:"short_id"`
	Kind     string `json:"kind"`
	PartKey  string `json:"part_key"`
	PartName string `json:"part_name"`
	Status   string `json:"status"`
	HeldBy   string `json:"held_by,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Color    string `json:"color,omitempty"`
	Location string `json:"location,omitempty"`
	Notes    string `json:"notes,omitempty"`
	For      string `json:"for,omitempty"`
}

// viewItem describes an item; inv may be nil for one just added (free).
func viewItem(c *catalog.Catalog, it *library.Item, inv *library.Inventory) itemView {
	v := itemView{ID: it.ID, ShortID: it.ShortID(), Kind: string(it.Kind), PartKey: it.PartKey, PartName: it.PartKey,
		Status: library.Free.String(), Nickname: it.Nickname, Color: it.Color, Location: it.Location, Notes: it.Notes, For: it.Dedicated}
	if p, ok := c.Part(it.PartKey); ok {
		v.PartName = p.Name
	}
	if inv != nil {
		v.Status, v.HeldBy = inv.Status[it.ID].String(), inv.HeldBy[it.ID]
	}
	return v
}

func viewItems(c *catalog.Catalog, items []*library.Item, inv *library.Inventory) []itemView {
	out := make([]itemView, len(items))
	for i, it := range items {
		out[i] = viewItem(c, it, inv)
	}
	return out
}
