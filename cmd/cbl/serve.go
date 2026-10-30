package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/advise"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/outline"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/route"
	"github.com/sonia-sharapova/cable-clutter/internal/smart"
)

//go:embed web
var webFiles embed.FS

func serveCmd() *cobra.Command {
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Browse your cables in a web page (local only)",
		Long: `Serve a small web page for your library on this computer only
(127.0.0.1). Search is instant: the page builds a hashmap of every prefix
of every name once, so each keystroke is a single lookup.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				return err
			}
			url := "http://" + ln.Addr().String()
			fmt.Fprintf(cmd.OutOrStdout(), "Cable Clutter is at %s  (Ctrl-C to stop)\n", url)
			if open {
				openBrowser(url)
			}
			return http.Serve(ln, newServer())
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "port to listen on")
	cmd.Flags().BoolVar(&open, "open", false, "open it in your browser")
	return cmd
}

func openBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	exec.Command(name, url).Start()
}

// newServer routes the page and its JSON API. Every request reloads the
// library, which takes about a millisecond, so the page never shows stale
// data after a CLI command.
func newServer() http.Handler {
	static, _ := fs.Sub(webFiles, "web")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/index", api(indexHandler))
	mux.HandleFunc("GET /api/part", api(partHandler))
	mux.HandleFunc("GET /api/connector", api(connectorHandler))
	mux.HandleFunc("GET /api/device", api(deviceHandler))
	mux.HandleFunc("GET /api/system", api(systemHandler))
	mux.HandleFunc("GET /api/connect", api(connectHandler))
	mux.HandleFunc("GET /api/wish", api(wishHandler))
	mux.HandleFunc("GET /api/inventory", api(inventoryHandler))
	mux.HandleFunc("POST /api/item/add", api(itemAddHandler))
	mux.HandleFunc("GET /api/builder", api(builderHandler))
	mux.HandleFunc("POST /api/system/new", api(systemNewHandler))
	mux.HandleFunc("POST /api/system/link", api(systemLinkHandler))
	mux.HandleFunc("POST /api/system/unlink", api(systemUnlinkHandler))
	mux.HandleFunc("POST /api/system/delete", api(systemDeleteHandler))
	mux.HandleFunc("POST /api/system/add-device", api(systemAddDeviceHandler))
	mux.HandleFunc("POST /api/system/remove-device", api(systemRemoveDeviceHandler))
	mux.HandleFunc("POST /api/system/up", api(systemUpHandler))
	mux.HandleFunc("POST /api/system/down", api(systemDownHandler))
	mux.HandleFunc("POST /api/wish/add", api(wishAddHandler))
	mux.HandleFunc("POST /api/wish/done", api(wishDoneHandler))
	mux.HandleFunc("GET /api/explore", api(exploreHandler))
	mux.HandleFunc("GET /api/devices", api(devicesHandler))
	mux.HandleFunc("POST /api/device/add", api(deviceAddHandler))
	mux.HandleFunc("POST /api/device/owner", api(deviceOwnerHandler))
	mux.HandleFunc("GET /api/parts-kit", api(partsKitHandler))
	mux.HandleFunc("POST /api/custom/add", api(customAddHandler))
	mux.HandleFunc("GET /api/bookmarks", api(bookmarksHandler))
	mux.HandleFunc("POST /api/bookmark/add", api(bookmarkAddHandler))
	mux.HandleFunc("POST /api/bookmark/delete", api(bookmarkDeleteHandler))
	return mux
}

// api wraps a handler with a fresh state and JSON output. Writes must carry
// the X-CBL header: a cross-site form can't set it, so other pages open in
// the browser can't change your library.
func api(h func(st *state, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		reply := func(code int, v any) {
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(v) // compact: the page is meant to be light
		}
		if r.Method == http.MethodPost && r.Header.Get("X-CBL") != "1" {
			reply(http.StatusForbidden, map[string]string{"error": "missing X-CBL header"})
			return
		}
		st, err := loadState()
		if err != nil {
			reply(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer st.db.Close()
		v, err := h(st, r)
		if err != nil {
			reply(http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reply(http.StatusOK, v)
	}
}

// entry is one searchable thing. The browser indexes every word start of
// every term into a prefix hashmap.
type entry struct {
	ID     string   `json:"id"` // what to open: "part:<key>", "device:<name>", …
	Kind   string   `json:"kind"`
	Title  string   `json:"title"`
	Sub    string   `json:"sub,omitempty"`
	Status string   `json:"status,omitempty"`
	Terms  []string `json:"terms"`
}

func indexHandler(st *state, _ *http.Request) (any, error) {
	start := time.Now()
	c := st.c
	var entries []entry
	endTerms := func(p *catalog.Part) []string {
		terms := []string{p.Name, p.Key}
		terms = append(terms, p.Aliases...)
		for _, e := range p.Ends {
			if cn, ok := c.Connectors[e.Connector]; ok {
				terms = append(terms, cn.Name)
				terms = append(terms, cn.Aliases...)
			}
		}
		return terms
	}

	// Your cables and adapters: one entry per type, with counts.
	counts := map[string]int{}
	for _, in := range []*library.Inventory{st.inv.Cables, st.inv.Adapters} {
		for _, key := range sortedKeys(in.Owned) {
			p, ok := c.Part(key)
			if !ok {
				continue
			}
			items := in.Owned[key]
			free, used := 0, 0
			terms := endTerms(p)
			var places []string
			for _, it := range items {
				switch in.Status[it.ID] {
				case library.Free:
					free++
				case library.InUse:
					used++
				}
				terms = append(terms, it.Nickname, it.Location, it.Dedicated, it.ShortID())
				if it.Location != "" && !contains(places, it.Location) {
					places = append(places, it.Location)
				}
			}
			counts[string(p.Kind)] += len(items)
			counts["in use"] += used
			status := "free"
			if used == len(items) {
				status = "in use"
			} else if free == 0 {
				status = "reserved"
			}
			sub := fmt.Sprintf("you own %d · %d available", len(items), free)
			if len(places) > 0 {
				sub += " · " + strings.Join(places, ", ")
			}
			entries = append(entries, entry{ID: "part:" + key, Kind: string(p.Kind), Title: p.Name, Sub: sub, Status: status, Terms: terms})
		}
	}
	// Your devices, then the catalog models you don't have.
	have := map[string]bool{}
	for _, d := range st.devices {
		sub := "no model yet"
		terms := []string{d.Nickname}
		if m, ok := c.Devices[d.Model]; ok {
			have[m.ID] = true
			sub = m.Name
			terms = append(append(terms, m.Name, m.ID), m.Aliases...)
		}
		entries = append(entries, entry{ID: "device:" + d.Nickname, Kind: "device", Title: d.Nickname, Sub: sub, Terms: terms})
	}
	counts["devices"] = len(st.devices)
	for _, s := range st.systems {
		status, terms := "down", []string{s.Name, "system", "setup"}
		if s.Active {
			status = "up"
		}
		for _, l := range st.links[s.ID] {
			terms = append(terms, l.FromDevice, l.ToDevice)
		}
		entries = append(entries, entry{ID: "system:" + s.Name, Kind: "system", Title: s.Name, Sub: fmt.Sprintf("%d links", len(st.links[s.ID])), Status: status, Terms: terms})
	}
	for _, m := range c.DeviceModels() {
		if !have[m.ID] {
			entries = append(entries, entry{ID: "model:" + m.ID, Kind: "model", Title: m.Name, Sub: "device model", Terms: append([]string{m.Name, m.ID}, m.Aliases...)})
		}
	}
	for _, kind := range []map[string]*catalog.Part{c.Cables, c.Adapters} {
		for _, key := range sortedKeys(kind) {
			if p := kind[key]; len(st.inv.Of(c, key).Owned[key]) == 0 {
				entries = append(entries, entry{ID: "part:" + key, Kind: "type", Title: p.Name, Sub: key, Terms: endTerms(p)})
			}
		}
	}
	for _, id := range sortedKeys(c.Connectors) {
		cn := c.Connectors[id]
		entries = append(entries, entry{ID: "connector:" + id, Kind: "connector", Title: cn.Name, Sub: id, Terms: append([]string{cn.Name, id}, cn.Aliases...)})
	}
	for i := range entries {
		entries[i].Terms = compact(entries[i].Terms)
	}
	var devices []string
	for _, d := range st.devices {
		if _, ok := c.Devices[d.Model]; ok {
			devices = append(devices, d.Nickname)
		}
	}
	return map[string]any{
		"catalog": c.Version, "entries": entries, "counts": counts, "devices": devices,
		"server_ms": float64(time.Since(start).Microseconds()) / 1000,
	}, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// compact drops empty and repeated terms.
func compact(terms []string) []string {
	seen := map[string]bool{}
	out := terms[:0]
	for _, t := range terms {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func partHandler(st *state, r *http.Request) (any, error) {
	key := r.URL.Query().Get("key")
	p, ok := st.c.Part(key)
	if !ok {
		return nil, fmt.Errorf("no cable or adapter type %q", key)
	}
	in := st.inv.Of(st.c, key)
	signals := []string{}
	for _, s := range p.Signals {
		signals = append(signals, st.c.Signals[s].Name)
	}
	return map[string]any{
		"part": viewPart(p), "outline": outline.Render(st.c, p, ""), "signals": signals,
		"items": viewItems(st.c, in.Owned[key], in),
	}, nil
}

func connectorHandler(st *state, r *http.Request) (any, error) {
	id := r.URL.Query().Get("id")
	cn, ok := st.c.Connectors[id]
	if !ok {
		return nil, fmt.Errorf("no connector %q", id)
	}
	type partOwned struct {
		partView
		Owned int `json:"owned"`
	}
	parts := []partOwned{}
	for _, p := range st.c.ByConnector[id] {
		parts = append(parts, partOwned{viewPart(p), len(st.inv.Of(st.c, p.Key).Owned[p.Key])})
	}
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].Owned > parts[j].Owned })
	return map[string]any{"connector": cn, "glyph": strings.Join(st.c.Glyph(id).Lines("m"), "\n"), "parts": parts}, nil
}

func deviceHandler(st *state, r *http.Request) (any, error) {
	m, nickname, hint, err := resolveModel(st.c, st.devices, r.URL.Query().Get("name"))
	if err != nil {
		return nil, err
	}
	out := deviceJSON(m, nickname, hint, advise.Device(st.c, m, st.inv.Cables, st.inv.Adapters, nickname))
	signals := map[string]string{}
	for id, s := range st.c.Signals {
		signals[id] = s.Name
	}
	out["signal_names"] = signals
	if found := library.FindDevices(st.devices, nickname); len(found) == 1 {
		out["owner"] = found[0].Owner
	}
	return out, nil
}

func systemHandler(st *state, r *http.Request) (any, error) {
	s, err := st.system(r.URL.Query().Get("name"))
	if err != nil {
		return nil, err
	}
	v := systemView(st, s, plan.Conflicts(st.c, plan.NewPorts(st.c, st.devices), st.systems, st.links, st.inv)[s.ID])
	names, items := map[string]string{}, map[string]string{}
	for _, l := range st.links[s.ID] {
		names[l.PartKey] = st.partName(l.PartKey)
		for _, id := range []string{l.Preferred, l.Assigned} {
			if it := st.item(id); it != nil {
				items[id] = it.ShortID()
			}
		}
	}
	v["part_names"], v["short_ids"] = names, items
	return v, nil
}

func connectHandler(st *state, r *http.Request) (any, error) {
	q := r.URL.Query()
	a, aName, _, err := resolveModel(st.c, st.devices, q.Get("from"))
	if err != nil {
		return nil, err
	}
	b, bName, _, err := resolveModel(st.c, st.devices, q.Get("to"))
	if err != nil {
		return nil, err
	}
	routes := route.Connect(st.c, a, b, st.inv, route.Options{})
	type buyJSON struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	buy := []buyJSON{}
	outlines := map[string]string{}
	for _, kind := range []string{"video", "audio", "data"} {
		rt, ok := routes[kind]
		if !ok {
			continue
		}
		for _, s := range rt.Steps {
			outlines[s.Part.Key] = outline.Render(st.c, s.Part, "")
			if s.Item == nil && !slices.ContainsFunc(buy, func(b buyJSON) bool { return b.Key == s.Part.Key }) {
				buy = append(buy, buyJSON{s.Part.Key, s.Part.Name})
			}
		}
	}
	signals := map[string]string{}
	for id, s := range st.c.Signals {
		signals[id] = s.Name
	}
	return map[string]any{"from": aName, "to": bName, "routes": routesJSON(routes), "to_buy": buy, "outlines": outlines, "signal_names": signals}, nil
}

func wishHandler(st *state, _ *http.Request) (any, error) {
	wishes, err := st.db.Wishes(true)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, w := range wishes {
		// What it's for: the system, else the note (e.g. "PS2 → CRT"), else -.
		use := w.System
		if use == "" {
			use = w.Notes
		}
		if use == "" {
			use = "-"
		}
		out = append(out, map[string]any{"id": w.ID, "name": st.partName(w.PartKey), "part": w.PartKey, "qty": w.Qty, "use_case": use, "done": w.Done})
	}
	return out, nil
}

func readBody(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("bad request body")
	}
	return nil
}

func systemUpHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Name   string `json:"name"`
		Switch bool   `json:"switch"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	s, err := st.system(req.Name)
	if err != nil {
		return nil, err
	}
	if s.Active {
		return map[string]any{"up": true}, nil
	}
	up, err := st.bringUp(s, req.Switch)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, m := range up.Missing {
		missing = append(missing, fmt.Sprintf("%d × %s (you own %d)", m.Need, st.partName(m.PartKey), m.Owned))
	}
	var blocked []string
	for _, sh := range up.Short {
		blocked = append(blocked, fmt.Sprintf("%s is in use by %s", st.partName(sh.Link.PartKey), quoteAll(sh.HeldBy)))
	}
	for _, b := range up.Busy {
		blocked = append(blocked, fmt.Sprintf("%s is taken by %s", b.Port, quoteAll(b.HeldBy)))
	}
	return map[string]any{"up": up.Picks != nil, "took_down": up.TookDown, "missing": missing, "blocked": blocked}, nil
}

func systemDownHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	s, err := st.system(req.Name)
	if err != nil {
		return nil, err
	}
	return map[string]any{"up": false}, st.db.TakeDown(s.ID)
}

// category groups a signal for the inventory filters: audio, video, data,
// network, storage or power.
func category(c *catalog.Catalog, signal string) string {
	switch {
	case strings.HasPrefix(signal, "net."):
		return "network"
	case strings.HasPrefix(signal, "storage."):
		return "storage"
	}
	return c.Signals[signal].Kind
}

// inventoryHandler lists every part type you own with its items and what
// the page filters on: kind, signal categories and connectors.
func inventoryHandler(st *state, _ *http.Request) (any, error) {
	type connJSON struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type rowJSON struct {
		Key        string     `json:"key"`
		Name       string     `json:"name"`
		Kind       string     `json:"kind"`
		Categories []string   `json:"categories"`
		Connectors []connJSON `json:"connectors"`
		Ends       []string   `json:"ends"`
		Outline    string     `json:"outline"`
		Items      []itemView `json:"items"`
	}
	rows := []rowJSON{}
	for _, in := range []*library.Inventory{st.inv.Cables, st.inv.Adapters} {
		for _, key := range sortedKeys(in.Owned) {
			p, ok := st.c.Part(key)
			if !ok {
				continue
			}
			row := rowJSON{Key: key, Name: p.Name, Kind: string(p.Kind), Categories: []string{}, Outline: outline.Render(st.c, p, ""), Items: viewItems(st.c, in.Owned[key], in)}
			for _, s := range p.Signals {
				if cat := category(st.c, s); !contains(row.Categories, cat) {
					row.Categories = append(row.Categories, cat)
				}
			}
			for _, e := range p.Ends {
				name := shortConnector(st.c, e.Connector)
				if e.Count > 1 {
					name += fmt.Sprintf(" ×%d", e.Count)
				}
				row.Ends = append(row.Ends, name)
				if !slices.ContainsFunc(row.Connectors, func(c connJSON) bool { return c.ID == e.Connector }) {
					row.Connectors = append(row.Connectors, connJSON{e.Connector, shortConnector(st.c, e.Connector)})
				}
			}
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

// itemAddHandler logs items you own: "I have one" on a cable's page.
func itemAddHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Part     string `json:"part"`
		Qty      int    `json:"qty"`
		Location string `json:"location"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	p, ok := st.c.Part(req.Part)
	if !ok {
		return nil, fmt.Errorf("no cable or adapter type %q", req.Part)
	}
	if req.Qty < 1 {
		req.Qty = 1
	}
	added, err := st.db.AddItems(library.Item{Kind: p.Kind, PartKey: p.Key, Location: req.Location}, req.Qty)
	if err != nil {
		return nil, err
	}
	return map[string]any{"added": viewItems(st.c, added, nil)}, nil
}

// wishAddHandler adds parts to the wishlist, skipping any already wished for.
func wishAddHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Parts []string `json:"parts"`
		Note  string   `json:"note"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	added := []string{}
	for _, key := range req.Parts {
		if _, ok := st.c.Part(key); !ok {
			return nil, fmt.Errorf("no cable or adapter type %q", key)
		}
		wanted, err := st.db.Wanted(key)
		if err != nil {
			return nil, err
		}
		if !wanted {
			if err := st.db.AddWish(key, 1, "", req.Note); err != nil {
				return nil, err
			}
			added = append(added, key)
		}
	}
	return map[string]any{"added": added}, nil
}

func wishDoneHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	wishes, err := st.db.Wishes(false)
	if err != nil {
		return nil, err
	}
	for _, w := range wishes {
		if w.ID == req.ID {
			kind := catalog.KindCable
			if p, ok := st.c.Part(w.PartKey); ok {
				kind = p.Kind
			}
			added, err := st.db.MarkWishDone(w, library.Item{Kind: kind})
			if err != nil {
				return nil, err
			}
			return map[string]any{"added": viewItems(st.c, added, nil)}, nil
		}
	}
	return nil, fmt.Errorf("no open wish %q", req.ID)
}

// builderHandler is what the system builder offers: each of your devices'
// ports with every catalog cable or adapter that fits it (not just yours),
// and the facts the page needs about each part.
func builderHandler(st *state, _ *http.Request) (any, error) {
	type portJSON struct {
		Label string   `json:"label"`
		End   string   `json:"end"`
		Dir   string   `json:"dir"`
		Fits  []string `json:"fits"`
	}
	type deviceJSON struct {
		Name  string     `json:"name"`
		Model string     `json:"model,omitempty"`
		Ports []portJSON `json:"ports"`
	}
	type partJSON struct {
		Name      string     `json:"name"`
		Kind      string     `json:"kind"`
		Uncommon  bool       `json:"uncommon,omitempty"`
		Available int        `json:"available"`
		Items     []itemView `json:"items"`
	}
	devices := []deviceJSON{}
	parts := map[string]partJSON{}
	for _, d := range st.devices {
		dj := deviceJSON{Name: d.Nickname, Ports: []portJSON{}}
		if m, ok := st.c.Devices[d.Model]; ok {
			dj.Model = m.ID
			for _, p := range m.Ports {
				pj := portJSON{Label: p.Label, End: p.End.String(), Dir: p.Dir, Fits: []string{}}
				for _, f := range st.c.Fits(p) {
					if f.WrongVoltage {
						continue
					}
					pj.Fits = append(pj.Fits, f.Part.Key)
					if _, done := parts[f.Part.Key]; !done {
						in := st.inv.Of(st.c, f.Part.Key)
						items := in.Owned[f.Part.Key]
						free := 0
						for _, it := range items {
							if in.Status[it.ID] == library.Free {
								free++
							}
						}
						parts[f.Part.Key] = partJSON{f.Part.Name, string(f.Part.Kind), st.c.Uncommon(f.Part), free, viewItems(st.c, items, in)}
					}
				}
				dj.Ports = append(dj.Ports, pj)
			}
		}
		devices = append(devices, dj)
	}
	return map[string]any{"devices": devices, "parts": parts}, nil
}

// linkRequest is one cable in a system: a part type, optionally one of your
// items, and the device ports it joins (ports may be left out).
type linkRequest struct {
	Part       string `json:"part"`
	Item       string `json:"item"`
	FromDevice string `json:"from_device"`
	FromPort   string `json:"from_port"`
	ToDevice   string `json:"to_device"`
	ToPort     string `json:"to_port"`
}

// link checks a requested link and turns it into one for system s.
func (st *state) link(s string, req linkRequest) (library.Link, error) {
	l := library.Link{SystemID: s, PartKey: req.Part, Preferred: req.Item}
	if _, ok := st.c.Part(req.Part); !ok {
		return l, fmt.Errorf("no cable or adapter type %q", req.Part)
	}
	if req.Item != "" && st.item(req.Item) == nil {
		return l, fmt.Errorf("no item %q", req.Item)
	}
	spec := func(device, port string) string {
		if device == "" || port == "" {
			return device
		}
		return device + ":" + port
	}
	var err error
	if l.FromDevice, l.FromPort, err = st.endpoint(spec(req.FromDevice, req.FromPort), req.Part); err != nil {
		return l, err
	}
	l.ToDevice, l.ToPort, err = st.endpoint(spec(req.ToDevice, req.ToPort), req.Part)
	return l, err
}

// systemNewHandler creates a system. With links, it uses exactly those.
// Without, it plans the chain of devices and saves the links open: any
// available cable of each type is used when the system is switched on.
func systemNewHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Name    string        `json:"name"`
		Devices []string      `json:"devices"`
		Links   []linkRequest `json:"links"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("give the system a name")
	}
	if len(req.Links) > 0 {
		var links []library.Link
		for _, lr := range req.Links {
			l, err := st.link("", lr)
			if err != nil {
				return nil, err
			}
			links = append(links, l)
		}
		s, err := st.db.NewSystem(req.Name, "")
		if err != nil {
			return nil, err
		}
		for _, l := range links {
			l.SystemID = s.ID
			if _, err := st.db.AddLink(l); err != nil {
				return nil, err
			}
		}
		return map[string]any{"name": s.Name, "links": len(links)}, nil
	}

	if len(req.Devices) < 2 {
		return nil, errors.New("pick at least two devices, or open the details and add the cables yourself")
	}
	var chain []smart.Device
	for _, name := range req.Devices {
		d, err := st.ownedDevice(name)
		if err != nil {
			return nil, err
		}
		chain = append(chain, d)
	}
	hops := smart.Chain(st.c, chain, st.inv, route.Options{})
	for _, h := range hops {
		if len(h.Routes) == 0 {
			return nil, fmt.Errorf("no way to connect %s to %s, even buying parts", h.From.Nickname, h.To.Nickname)
		}
	}
	s, err := st.savePlan(req.Name, hops, false)
	if err != nil {
		return nil, err
	}
	links, _ := st.db.Links()
	return map[string]any{"name": s.Name, "links": len(links[s.ID])}, nil
}

func systemLinkHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		System string `json:"system"`
		linkRequest
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	s, err := st.system(req.System)
	if err != nil {
		return nil, err
	}
	l, err := st.link(s.ID, req.linkRequest)
	if err != nil {
		return nil, err
	}
	added, err := st.db.AddLink(l)
	if err != nil {
		return nil, err
	}
	return linkView(added), nil
}

func systemUnlinkHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	for _, s := range st.systems {
		for _, l := range st.links[s.ID] {
			if l.ID == req.ID {
				if s.Active {
					return nil, fmt.Errorf("%q is on; switch it off first", s.Name)
				}
				return map[string]string{"removed": l.ID}, st.db.RemoveLink(l.ID)
			}
		}
	}
	return nil, fmt.Errorf("no link %q", req.ID)
}

func systemDeleteHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	s, err := st.system(req.Name)
	if err != nil {
		return nil, err
	}
	return map[string]string{"removed": s.ID}, st.db.RemoveSystem(s)
}

type bookmarkJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

func bookmarksHandler(st *state, _ *http.Request) (any, error) {
	marks, err := st.db.Bookmarks()
	if err != nil {
		return nil, err
	}
	out := []bookmarkJSON{}
	for _, b := range marks {
		out = append(out, bookmarkJSON{b.ID, b.Name, b.From, b.To})
	}
	return out, nil
}

// bookmarkAddHandler saves a route from the Route page. Both ends must be
// devices the route finder can use, so a bookmark always opens.
func bookmarkAddHandler(st *state, r *http.Request) (any, error) {
	var req bookmarkJSON
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	for _, ref := range []string{req.From, req.To} {
		if _, _, _, err := resolveModel(st.c, st.devices, ref); err != nil {
			return nil, err
		}
	}
	b, err := st.db.AddBookmark(strings.TrimSpace(req.Name), req.From, req.To)
	if err != nil {
		return nil, err
	}
	return bookmarkJSON{b.ID, b.Name, b.From, b.To}, nil
}

func bookmarkDeleteHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	return map[string]string{"removed": req.ID}, st.db.RemoveBookmark(req.ID)
}

// partsKitHandler is what the "add something new" form picks from: every
// connector and signal in the catalog.
func partsKitHandler(st *state, _ *http.Request) (any, error) {
	type option struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind,omitempty"` // a connector's family, a signal's kind
	}
	connectors, signals := []option{}, []option{}
	for _, cn := range st.c.Connectors {
		connectors = append(connectors, option{cn.ID, cn.Name, cn.Family})
	}
	for _, sg := range st.c.Signals {
		signals = append(signals, option{sg.ID, sg.Name, sg.Kind})
	}
	sort.Slice(connectors, func(i, j int) bool { return strings.ToLower(connectors[i].Name) < strings.ToLower(connectors[j].Name) })
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Kind != signals[j].Kind {
			return signals[i].Kind < signals[j].Kind
		}
		return signals[i].Name < signals[j].Name
	})
	generics := []option{}
	for _, m := range st.c.DeviceModels() {
		if m.Generic {
			generics = append(generics, option{m.ID, strings.TrimSuffix(m.Name, " (generic)"), m.Category})
		}
	}
	return map[string]any{"connectors": connectors, "signals": signals, "generics": generics}, nil
}

// customAddHandler adds a cable, adapter or device the catalog doesn't
// have: it's stored as your own catalog entry, then logged as yours.
func customAddHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Kind string `json:"kind"` // cable, adapter or device
		catalog.CustomPart
		Ports    []catalog.CustomPort `json:"ports"`
		Generic  string               `json:"generic"` // a generic model to take the ports from
		Make     string               `json:"make"`
		Wireless string               `json:"wireless"`
		Owner    string               `json:"owner"`
		Qty      int                  `json:"qty"`
		Location string               `json:"location"`
		Nickname string               `json:"nickname"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	req.Name = strings.TrimSpace(req.Name)
	switch req.Kind {
	case "cable", "adapter":
		kind := catalog.Kind(req.Kind)
		key, err := st.c.CheckPart(kind, req.CustomPart)
		if err != nil {
			return nil, err
		}
		body, _ := json.Marshal(req.CustomPart)
		if err := st.db.AddCustom(library.CustomEntry{Kind: req.Kind, ID: key, Body: body}); err != nil {
			return nil, err
		}
		if req.Qty < 1 {
			req.Qty = 1
		}
		added, err := st.db.AddItems(library.Item{Kind: kind, PartKey: key, Location: req.Location, Nickname: req.Nickname}, req.Qty)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": req.Kind, "key": key, "name": req.Name, "added": len(added)}, nil
	case "device":
		d := catalog.CustomDevice{ID: customDeviceID(st.c, req.Name), Name: req.Name, Make: strings.TrimSpace(req.Make),
			Wireless: strings.TrimSpace(req.Wireless), Ports: req.Ports}
		if req.Name == "" {
			return nil, errors.New("give the device a name")
		}
		if req.Generic != "" {
			// You don't know its ports: assume a typical one of its kind.
			g, ok := st.c.Devices[req.Generic]
			if !ok || !g.Generic {
				return nil, fmt.Errorf("no generic device type %q", req.Generic)
			}
			d.Category, d.Ports = g.Category, nil
			if d.Wireless == "" {
				d.Wireless = g.Wireless
			}
			d.Assumption = "ports of a typical " + strings.ToLower(strings.TrimSuffix(g.Name, " (generic)")) + ": " + g.Assumption
			for _, p := range g.Ports {
				d.Ports = append(d.Ports, catalog.CustomPort{Label: p.Label, End: p.End.String(), Qty: p.Qty, Dir: p.Dir, Signals: p.Signals, Volts: p.Volts})
			}
		}
		if err := st.c.CheckDevice(d); err != nil {
			return nil, err
		}
		nickname := strings.TrimSpace(req.Nickname)
		if nickname == "" {
			nickname = d.Name
		}
		body, _ := json.Marshal(d)
		if err := st.db.AddCustom(library.CustomEntry{Kind: "device", ID: d.ID, Body: body}); err != nil {
			return nil, err
		}
		dev, _, err := st.db.AddDevice(d.ID, nickname, "")
		if err != nil {
			return nil, err
		}
		if req.Owner != "" {
			if err := st.db.SetOwner(dev.ID, req.Owner); err != nil {
				return nil, err
			}
		}
		return map[string]any{"kind": "device", "model": d.ID, "name": dev.Nickname}, nil
	}
	return nil, fmt.Errorf("kind must be cable, adapter or device, not %q", req.Kind)
}

// customDeviceID makes a model id from a name, e.g. "My Gizmo" →
// custom-my-gizmo, numbered if that's taken.
func customDeviceID(c *catalog.Catalog, name string) string {
	slug := strings.Join(strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}), "-")
	id := "custom-" + slug
	for n := 2; c.Devices[id] != nil; n++ {
		id = fmt.Sprintf("custom-%s-%d", slug, n)
	}
	return id
}

// exploreHandler is the whole catalog of cables and adapters, filed by port
// type: each connector family A–Z, its connectors A–Z, and every part with
// an end of that connector, with how many of each you own.
func exploreHandler(st *state, _ *http.Request) (any, error) {
	type partJSON struct {
		Key   string `json:"key"`
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		Owned int    `json:"owned"`
	}
	type connJSON struct {
		ID    string     `json:"id"`
		Name  string     `json:"name"`
		Parts []partJSON `json:"parts"`
	}
	type familyJSON struct {
		Family     string     `json:"family"`
		Connectors []connJSON `json:"connectors"`
	}
	owned := func(p *catalog.Part) int {
		if p.Kind == catalog.KindAdapter {
			return len(st.inv.Adapters.Owned[p.Key])
		}
		return len(st.inv.Cables.Owned[p.Key])
	}
	byFamily := map[string][]connJSON{}
	for id, cn := range st.c.Connectors {
		parts := st.c.ByConnector[id]
		if len(parts) == 0 {
			continue
		}
		cj := connJSON{ID: id, Name: cn.Name}
		for _, p := range parts {
			cj.Parts = append(cj.Parts, partJSON{p.Key, p.Name, string(p.Kind), owned(p)})
		}
		sort.Slice(cj.Parts, func(i, j int) bool { return strings.ToLower(cj.Parts[i].Name) < strings.ToLower(cj.Parts[j].Name) })
		family := cn.Family
		if family == "" {
			family = "other"
		}
		byFamily[family] = append(byFamily[family], cj)
	}
	out := []familyJSON{}
	for _, f := range sortedKeys(byFamily) {
		conns := byFamily[f]
		sort.Slice(conns, func(i, j int) bool { return strings.ToLower(conns[i].Name) < strings.ToLower(conns[j].Name) })
		out = append(out, familyJSON{f, conns})
	}
	return out, nil
}

// systemAddDeviceHandler plugs another of your devices into a system: it
// plans the cables between it and a device already there, trying the new
// one's outputs first (a keyboard into a computer), then its inputs (a
// computer into a speaker).
func systemAddDeviceHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		System string `json:"system"`
		Device string `json:"device"`
		To     string `json:"to"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	s, err := st.system(req.System)
	if err != nil {
		return nil, err
	}
	if s.Active {
		return nil, fmt.Errorf("%q is on; switch it off first", s.Name)
	}
	a, err := st.ownedDevice(req.Device)
	if err != nil {
		return nil, err
	}
	b, err := st.ownedDevice(req.To)
	if err != nil {
		return nil, err
	}
	found := func(hops []smart.Hop) bool { return len(hops) > 0 && len(hops[0].Routes) > 0 }
	hops := smart.Chain(st.c, []smart.Device{a, b}, st.inv, route.Options{})
	if !found(hops) {
		hops = smart.Chain(st.c, []smart.Device{b, a}, st.inv, route.Options{})
	}
	if !found(hops) {
		return nil, fmt.Errorf("no way to connect %s and %s, even buying parts", a.Nickname, b.Nickname)
	}
	if err := st.addHops(s, hops, false); err != nil {
		return nil, err
	}
	return map[string]string{"added": a.Nickname}, nil
}

// systemRemoveDeviceHandler takes a device out of a system with every
// cable chain that reaches it, adapters in the middle included.
func systemRemoveDeviceHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		System string `json:"system"`
		Device string `json:"device"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	s, err := st.system(req.System)
	if err != nil {
		return nil, err
	}
	if s.Active {
		return nil, fmt.Errorf("%q is on; switch it off first", s.Name)
	}
	removed := 0
	for _, chain := range linkChains(st.links[s.ID]) {
		if !slices.ContainsFunc(chain, func(l *library.Link) bool {
			return strings.EqualFold(l.FromDevice, req.Device) || strings.EqualFold(l.ToDevice, req.Device)
		}) {
			continue
		}
		for _, l := range chain {
			if err := st.db.RemoveLink(l.ID); err != nil {
				return nil, err
			}
			removed++
		}
	}
	if removed == 0 {
		return nil, fmt.Errorf("%q isn't in %q", req.Device, s.Name)
	}
	return map[string]int{"removed": removed}, nil
}

// linkChains groups a system's links, in the order they were saved, into
// chains: a planned route saves its first link with where it starts and its
// last with where it ends, the ones between with neither.
func linkChains(links []*library.Link) [][]*library.Link {
	var chains [][]*library.Link
	var cur []*library.Link
	for _, l := range links {
		if l.FromDevice != "" && len(cur) > 0 {
			chains, cur = append(chains, cur), nil
		}
		cur = append(cur, l)
		if l.ToDevice != "" {
			chains, cur = append(chains, cur), nil
		}
	}
	if len(cur) > 0 {
		chains = append(chains, cur)
	}
	return chains
}

// devicesHandler is your devices with make, model and owner, and every
// catalog model you could add, named ones first, then generic types.
func devicesHandler(st *state, _ *http.Request) (any, error) {
	type modelJSON struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Make     string `json:"make,omitempty"`
		Category string `json:"category,omitempty"`
		Generic  bool   `json:"generic,omitempty"`
	}
	type deviceJSON struct {
		Name  string    `json:"name"`
		Owner string    `json:"owner"`
		Model modelJSON `json:"model"`
	}
	view := func(m *catalog.DeviceModel) modelJSON { return modelJSON{m.ID, m.Name, m.Make, m.Category, m.Generic} }
	devices := []deviceJSON{}
	for _, d := range st.devices {
		dj := deviceJSON{Name: d.Nickname, Owner: d.Owner}
		if m, ok := st.c.Devices[d.Model]; ok {
			dj.Model = view(m)
		}
		devices = append(devices, dj)
	}
	models := []modelJSON{}
	for _, m := range st.c.DeviceModels() {
		models = append(models, view(m))
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Generic != models[j].Generic {
			return !models[i].Generic
		}
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})
	return map[string]any{"devices": devices, "models": models}, nil
}

// deviceAddHandler records a device you own from a catalog model.
func deviceAddHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Model    string `json:"model"`
		Nickname string `json:"nickname"`
		Owner    string `json:"owner"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	m, ok := st.c.Devices[req.Model]
	if !ok {
		return nil, fmt.Errorf("no device model %q", req.Model)
	}
	name := strings.TrimSpace(req.Nickname)
	if name == "" {
		name = m.Name
	}
	d, _, err := st.db.AddDevice(m.ID, name, "")
	if err != nil {
		return nil, err
	}
	if req.Owner != "" {
		if err := st.db.SetOwner(d.ID, req.Owner); err != nil {
			return nil, err
		}
	}
	return map[string]string{"name": d.Nickname}, nil
}

func deviceOwnerHandler(st *state, r *http.Request) (any, error) {
	var req struct {
		Name  string `json:"name"`
		Owner string `json:"owner"`
	}
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	found := library.FindDevices(st.devices, req.Name)
	if len(found) != 1 {
		return nil, fmt.Errorf("no single device called %q", req.Name)
	}
	return map[string]string{"owner": req.Owner}, st.db.SetOwner(found[0].ID, req.Owner)
}
