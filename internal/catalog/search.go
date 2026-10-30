package catalog

import (
	"sort"
	"strings"
)

type RefKind int

// Search results are ranked in this order within each match tier.
const (
	RefConnector RefKind = iota
	RefCable
	RefAdapter
	RefSignal
)

// Ref names one catalog entry: a connector or signal ID, or a part key.
type Ref struct {
	Kind RefKind
	ID   string
}

type term struct {
	text string
	ref  Ref
}

// Normalize folds what people type into one form: lower case, with spaces,
// hyphens, underscores, slashes and brackets removed. "USB-C", "usb c" and
// "usbc" all become "usbc"; "3.5 mm" becomes "3.5mm".
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '-', '_', '/', '(', ')', ',':
		case '×':
			b.WriteRune('x')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (c *Catalog) buildSearch(signals []Signal, connectors []Connector, parts []*Part) {
	seenAlias, seenTerm := map[term]bool{}, map[term]bool{}
	add := func(ref Ref, names ...string) {
		for _, name := range names {
			if n := Normalize(name); n != "" && !seenAlias[term{n, ref}] {
				seenAlias[term{n, ref}] = true
				c.aliases[n] = append(c.aliases[n], ref)
			}
			// Index every word start, so "lightning" finds "USB-A to Lightning".
			words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
				return strings.ContainsRune(" -_/(),", r)
			})
			for i := range words {
				t := term{Normalize(strings.Join(words[i:], "")), ref}
				if t.text != "" && !seenTerm[t] {
					seenTerm[t] = true
					c.terms = append(c.terms, t)
				}
			}
		}
	}
	for _, cn := range connectors {
		add(Ref{RefConnector, cn.ID}, append([]string{cn.ID, cn.Name}, cn.Aliases...)...)
	}
	for _, p := range parts {
		kind := RefCable
		if p.Kind == KindAdapter {
			kind = RefAdapter
		}
		add(Ref{kind, p.Key}, append([]string{p.Name}, p.Aliases...)...)
	}
	for _, s := range signals {
		add(Ref{RefSignal, s.ID}, s.ID, s.Name)
	}
	sort.Slice(c.terms, func(i, j int) bool { return c.terms[i].text < c.terms[j].text })
}

// Find returns catalog entries matching q: exact name or alias matches first,
// then entries with a word starting with q. Within each tier, connectors come
// before cables, adapters and signals.
func (c *Catalog) Find(q string) []Ref {
	n := Normalize(q)
	if n == "" {
		return nil
	}
	seen := map[Ref]bool{}
	var out []Ref

	exact := append([]Ref(nil), c.aliases[n]...)
	sort.SliceStable(exact, func(i, j int) bool { return exact[i].Kind < exact[j].Kind })
	for _, r := range exact {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}

	var prefix []Ref
	for i := sort.Search(len(c.terms), func(i int) bool { return c.terms[i].text >= n }); i < len(c.terms) && strings.HasPrefix(c.terms[i].text, n); i++ {
		if r := c.terms[i].ref; !seen[r] {
			seen[r] = true
			prefix = append(prefix, r)
		}
	}
	sort.SliceStable(prefix, func(i, j int) bool { return prefix[i].Kind < prefix[j].Kind })
	return append(out, prefix...)
}

// ResolveConnector maps a connector ID or alias ("aux", "USB C") to its ID.
func (c *Catalog) ResolveConnector(s string) (string, bool) {
	if _, ok := c.Connectors[s]; ok {
		return s, true
	}
	for _, r := range c.aliases[Normalize(s)] {
		if r.Kind == RefConnector {
			return r.ID, true
		}
	}
	return "", false
}
