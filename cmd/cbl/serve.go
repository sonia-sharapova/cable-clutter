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
	mux.HandleFunc("POST /api/system/up", api(systemUpHandler))
	mux.HandleFunc("POST /api/system/down", api(systemDownHandler))
	mux.HandleFunc("POST /api/wish/add", api(wishAddHandler))
	mux.HandleFunc("POST /api/wish/done", api(wishDoneHandler))
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
			sub := fmt.Sprintf("you own %d · %d free", len(items), free)
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
	wishes, err := st.db.Wishes(false)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, w := range wishes {
		out = append(out, map[string]any{"id": w.ID, "name": st.partName(w.PartKey), "part": w.PartKey, "qty": w.Qty, "for": w.System, "notes": w.Notes})
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
