package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/advise"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

func deviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device <name|model>",
		Short: "A device's ports, what they do, and what you can and can't connect",
		Example: `  cbl device ps2            # your device by name, or a catalog model
  cbl device "macbook pro"
  cbl device add ps2-slim --name PS2
  cbl device ls`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return showDevice(cmd, strings.Join(args, " "))
		},
	}
	cmd.AddCommand(deviceAddCmd(), deviceLsCmd(), deviceRmCmd())
	return cmd
}

// resolveModel finds the model a person means: one of their devices by name,
// else a catalog model. nickname is set when it's one of theirs.
// hint, when set, is a note to show alongside the model.
func resolveModel(c *catalog.Catalog, devices []*library.Device, ref string) (model *catalog.DeviceModel, nickname, hint string, err error) {
	if found := library.FindDevices(devices, ref); len(found) > 0 {
		if len(found) > 1 {
			return nil, "", "", fmt.Errorf("%q matches %d of your devices; use an id from `cbl device ls`", ref, len(found))
		}
		d := found[0]
		if d.Model == "" {
			// A placeholder from `cbl add --for`: fall back to the catalog.
			if models := c.ResolveDevice(ref); len(models) == 1 {
				hint = fmt.Sprintf("Your %q has no model yet; to link it: cbl device add %s --name %q", d.Nickname, models[0].ID, d.Nickname)
				return models[0], d.Nickname, hint, nil
			}
			return nil, "", "", fmt.Errorf("%q has no model yet; set one with `cbl device add <model> --name %q` (see `cbl catalog devices`)", d.Nickname, d.Nickname)
		}
		m, ok := c.Devices[d.Model]
		if !ok {
			return nil, "", "", fmt.Errorf("%q is a %s, which the catalog no longer knows", d.Nickname, d.Model)
		}
		return m, d.Nickname, "", nil
	}
	models := c.ResolveDevice(ref)
	switch len(models) {
	case 0:
		return nil, "", "", fmt.Errorf("no device or model called %q; see `cbl device ls` or `cbl catalog devices`", ref)
	case 1:
		for _, d := range devices { // a catalog model you own: use your name for it
			if d.Model == models[0].ID {
				return models[0], d.Nickname, "", nil
			}
		}
		return models[0], "", "", nil
	}
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = fmt.Sprintf("%s (%s)", m.Name, m.ID)
	}
	return nil, "", "", fmt.Errorf("%q could be: %s", ref, strings.Join(names, ", "))
}

func showDevice(cmd *cobra.Command, ref string) error {
	c, err := loadCatalog()
	if err != nil {
		return err
	}
	db, err := openLibrary()
	if err != nil {
		return err
	}
	defer db.Close()
	devices, err := db.Devices()
	if err != nil {
		return err
	}
	model, nickname, hint, err := resolveModel(c, devices, ref)
	if err != nil {
		return err
	}
	cables, err := db.LoadInventory(catalog.KindCable)
	if err != nil {
		return err
	}
	adapters, err := db.LoadInventory(catalog.KindAdapter)
	if err != nil {
		return err
	}
	reports := advise.Device(c, model, cables, adapters, nickname)
	if jsonOut {
		return emit(cmd, deviceJSON(model, nickname, hint, reports))
	}
	printDevice(cmd.OutOrStdout(), c, model, nickname, reports)
	if hint != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", hint)
	}
	return nil
}

func printDevice(w io.Writer, c *catalog.Catalog, m *catalog.DeviceModel, nickname string, ports []advise.PortReport) {
	head := m.Name + "  ·  " + m.ID
	if nickname != "" {
		head += fmt.Sprintf("  ·  your %q", nickname)
	}
	fmt.Fprintln(w, head)
	if !m.Confirmed {
		fmt.Fprintf(w, "Ports not confirmed: assumes %s.\n", m.Assumption)
	}
	if len(ports) == 0 {
		fmt.Fprintln(w, "\nNo cable ports.")
		return
	}
	signals := func(ids []string) string {
		names := make([]string, len(ids))
		for i, s := range ids {
			names[i] = c.Signals[s].Name
		}
		return strings.Join(names, ", ")
	}
	ids := func(o advise.Owned) string {
		var out []string
		for _, it := range o.Items {
			out = append(out, "#"+it.ShortID())
		}
		return strings.Join(out, " ")
	}
	for _, r := range ports {
		p := r.Port
		title := p.Label
		if p.Qty > 1 {
			title += fmt.Sprintf(" x%d", p.Qty)
		}
		socket := shortConnector(c, p.End.Connector)
		if p.End.Count > 1 {
			socket += fmt.Sprintf(" x%d", p.End.Count)
		}
		fmt.Fprintf(w, "\n%s · %s (%s) · %s\n", title, socket, strings.ToUpper(p.End.Gender), p.Dir)
		fmt.Fprintf(w, "  Does:      %s\n", signals(p.Signals))
		if p.Note != "" {
			fmt.Fprintf(w, "  Note:      %s\n", p.Note)
		}
		if len(r.Have) == 0 {
			fmt.Fprintln(w, "  Can:       nothing yet; you own nothing that fits")
		} else {
			fmt.Fprintf(w, "  Can:       %s\n", signals(r.Can))
			for i, o := range r.Have {
				if i == 5 {
					fmt.Fprintf(w, "             … and %d more you own\n", len(r.Have)-5)
					break
				}
				line := fmt.Sprintf("             ✓ %s", o.Fit.Part.Name)
				if len(o.Items) > 1 {
					line += fmt.Sprintf(" x%d", len(o.Items))
				}
				switch {
				case o.Free == 0:
					line += " (none free)"
				case o.Free < len(o.Items):
					line += fmt.Sprintf(" (%d free)", o.Free)
				}
				if o.Dedicated {
					line += " · dedicated"
				}
				if o.Fit.Part.Kind == catalog.KindAdapter {
					line += " · adapter"
				}
				fmt.Fprintln(w, line)
			}
		}
		for i, s := range r.CantYet {
			label := "  Can't yet: "
			if i > 0 {
				label = "             "
			}
			fmt.Fprintf(w, "%s%s → %s\n", label, signals(s.Signals), s.Part.Name)
		}
		for _, o := range r.Wrong {
			fmt.Fprintf(w, "  ⚠ Don't use %s (%s): it's %gV, this port needs %gV\n", o.Fit.Part.Name, ids(o), o.Fit.Part.Volts, p.Volts)
		}
		for _, o := range r.Unsure {
			fmt.Fprintf(w, "  ? %s (%s): voltage unknown; this port needs %gV, check the label first\n", o.Fit.Part.Name, ids(o), p.Volts)
		}
	}
}

type ownedView struct {
	Part      partView `json:"part"`
	Items     []string `json:"items"`
	Free      int      `json:"free"`
	Dedicated bool     `json:"dedicated,omitempty"`
}

func viewOwned(os []advise.Owned) []ownedView {
	out := []ownedView{}
	for _, o := range os {
		v := ownedView{Part: viewPart(o.Fit.Part), Free: o.Free, Dedicated: o.Dedicated}
		for _, it := range o.Items {
			v.Items = append(v.Items, it.ID)
		}
		out = append(out, v)
	}
	return out
}

func deviceJSON(m *catalog.DeviceModel, nickname, hint string, reports []advise.PortReport) map[string]any {
	type suggestion struct {
		Part    partView `json:"part"`
		Signals []string `json:"signals"`
	}
	type portView struct {
		catalog.Port
		Can     []string     `json:"can"`
		Have    []ownedView  `json:"have"`
		CantYet []suggestion `json:"cant_yet"`
		Wrong   []ownedView  `json:"wrong_voltage"`
		Unsure  []ownedView  `json:"unknown_voltage"`
	}
	ports := []portView{}
	for _, r := range reports {
		pv := portView{Port: r.Port, Can: r.Can, Have: viewOwned(r.Have), Wrong: viewOwned(r.Wrong), Unsure: viewOwned(r.Unsure), CantYet: []suggestion{}}
		if pv.Can == nil {
			pv.Can = []string{}
		}
		for _, s := range r.CantYet {
			pv.CantYet = append(pv.CantYet, suggestion{viewPart(s.Part), s.Signals})
		}
		ports = append(ports, pv)
	}
	out := map[string]any{"model": m.ID, "name": m.Name, "confirmed": m.Confirmed, "ports": ports}
	if !m.Confirmed {
		out["assumption"] = m.Assumption
	}
	if nickname != "" {
		out["nickname"] = nickname
	}
	if hint != "" {
		out["hint"] = hint
	}
	return out
}

func shortConnector(c *catalog.Catalog, id string) string {
	cn, ok := c.Connectors[id]
	if !ok {
		return id
	}
	name, _, _ := strings.Cut(cn.Name, " (")
	return name
}

func deviceAddCmd() *cobra.Command {
	var name, notes string
	cmd := &cobra.Command{
		Use:   "add <model>",
		Short: "Record a device you own (gives a model to a placeholder of the same name)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := loadCatalog()
			if err != nil {
				return err
			}
			ref := strings.Join(args, " ")
			models := c.ResolveDevice(ref)
			if len(models) != 1 {
				_, _, _, err := resolveModel(c, nil, ref)
				return err
			}
			m := models[0]
			if name == "" {
				name = m.Name
			}
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			d, created, err := db.AddDevice(m.ID, name, notes)
			if err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]any{"id": d.ID, "short_id": d.ShortID(), "nickname": d.Nickname, "model": m.ID, "created": created})
			}
			verb := "Added"
			if !created {
				verb = "Set the model of"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %q: %s (#%s)\n", verb, d.Nickname, m.Name, d.ShortID())
			if !m.Confirmed {
				fmt.Fprintf(cmd.OutOrStdout(), "Ports not confirmed: assumes %s.\n", m.Assumption)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "what you call it (default: the model name)")
	cmd.Flags().StringVar(&notes, "notes", "", "free-form notes")
	return cmd
}

func deviceLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List your devices",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := loadCatalog()
			if err != nil {
				return err
			}
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			devices, err := db.Devices()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				type deviceView struct {
					ID        string `json:"id"`
					ShortID   string `json:"short_id"`
					Nickname  string `json:"nickname"`
					Model     string `json:"model,omitempty"`
					Confirmed bool   `json:"confirmed"`
					Notes     string `json:"notes,omitempty"`
				}
				views := []deviceView{}
				for _, d := range devices {
					m, ok := c.Devices[d.Model]
					views = append(views, deviceView{d.ID, d.ShortID(), d.Nickname, d.Model, ok && m.Confirmed, d.Notes})
				}
				return emit(cmd, views)
			}
			if len(devices) == 0 {
				fmt.Fprintln(out, "No devices yet. Add one with `cbl device add <model> --name <name>`.")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tMODEL\tPORTS\tNOTES")
			for _, d := range devices {
				model, ports, note := "(none yet)", "", d.Notes
				if m, ok := c.Devices[d.Model]; ok {
					model = m.Name
					n := 0
					for _, p := range m.Ports {
						n += p.Qty
					}
					ports = fmt.Sprint(n)
					if !m.Confirmed {
						note = strings.TrimSpace("ports unconfirmed " + note)
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", d.ShortID(), d.Nickname, model, ports, note)
			}
			return tw.Flush()
		},
	}
}

func deviceRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id|name>",
		Short: "Remove a device (its dedicated cables stay, no longer dedicated)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			devices, err := db.Devices()
			if err != nil {
				return err
			}
			ref := strings.Join(args, " ")
			found := library.FindDevices(devices, ref)
			if len(found) != 1 {
				return fmt.Errorf("%q matches %d devices; use an id from `cbl device ls`", ref, len(found))
			}
			if err := db.RemoveDevice(found[0].ID); err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]string{"removed": found[0].ID})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %q\n", found[0].Nickname)
			return nil
		},
	}
}
