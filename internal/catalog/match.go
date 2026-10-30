package catalog

import (
	"fmt"
	"strconv"
	"strings"
)

// EndQuery is an end as a person types it: gender and plug count are optional.
type EndQuery struct {
	Connector string
	Gender    string // "" = either
	Count     int    // 0 = any
}

// ParseEndQuery reads "usb-c", "usb-c:f", "rca*3", "aux" or "3.5mm:m",
// resolving connector aliases.
func (c *Catalog) ParseEndQuery(s string) (EndQuery, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	var q EndQuery
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		if g := s[i+1:]; g == "m" || g == "f" {
			s, q.Gender = s[:i], g
		}
	}
	if name, n, ok := strings.Cut(s, "*"); ok {
		count, err := strconv.Atoi(n)
		if err != nil || count < 1 {
			return EndQuery{}, fmt.Errorf("bad plug count in %q", s)
		}
		s, q.Count = name, count
	}
	id, ok := c.ResolveConnector(s)
	if !ok {
		return EndQuery{}, fmt.Errorf("unknown connector %q", s)
	}
	q.Connector = id
	return q, nil
}

func (q EndQuery) matches(e End) bool {
	return q.Connector == e.Connector &&
		(q.Gender == "" || q.Gender == e.Gender) &&
		(q.Count == 0 || q.Count == e.Count)
}

// MatchParts returns the parts of this kind whose ends match qs one-to-one,
// in any order. A non-empty variant narrows the result to that variant.
// An end typed without a plug count means one plug if any part fits that
// way ("aux aux" is the aux cable, not the splitter); otherwise any count.
func (c *Catalog) MatchParts(kind Kind, qs []EndQuery, variant string) []*Part {
	singles := make([]EndQuery, len(qs))
	for i, q := range qs {
		if q.Count == 0 {
			q.Count = 1
		}
		singles[i] = q
	}
	if found := c.matchParts(kind, singles, variant); len(found) > 0 {
		return found
	}
	return c.matchParts(kind, qs, variant)
}

func (c *Catalog) matchParts(kind Kind, qs []EndQuery, variant string) []*Part {
	if len(qs) == 0 {
		return nil
	}
	var out []*Part
	for _, p := range c.ByConnector[qs[0].Connector] {
		if p.Kind != kind || len(p.Ends) != len(qs) {
			continue
		}
		if variant != "" && !strings.EqualFold(p.Variant, variant) {
			continue
		}
		if assign(qs, p.Ends, make([]bool, len(p.Ends))) {
			out = append(out, p)
		}
	}
	return out
}

// assign reports whether every query can take a distinct end. Parts have at
// most a handful of ends, so plain backtracking is plenty.
func assign(qs []EndQuery, ends []End, used []bool) bool {
	if len(qs) == 0 {
		return true
	}
	for i, e := range ends {
		if !used[i] && qs[0].matches(e) {
			used[i] = true
			if assign(qs[1:], ends, used) {
				return true
			}
			used[i] = false
		}
	}
	return false
}

// Resolve picks the part a person means, from either a list of ends
// ("usb-a usb-c", "aux rca") or a name ("aux cable", "USB-C hub"). It returns
// one part, or every candidate when the input is ambiguous or unknown.
func (c *Catalog) Resolve(kind Kind, args []string, variant string) (*Part, []*Part) {
	if len(args) == 1 {
		if p, ok := c.Part(args[0]); ok && p.Kind == kind {
			return p, nil
		}
	}
	if len(args) >= 2 {
		qs := make([]EndQuery, 0, len(args))
		for _, a := range args {
			q, err := c.ParseEndQuery(a)
			if err != nil {
				qs = nil
				break
			}
			qs = append(qs, q)
		}
		if qs != nil {
			found := c.MatchParts(kind, qs, variant)
			if len(found) == 1 {
				return found[0], nil
			}
			return nil, found
		}
	}

	// Not a list of ends: treat the words as a name. An exact name or alias
	// wins; otherwise fall back to word-start search.
	name := strings.Join(args, " ")
	var exact, all []*Part
	for _, r := range c.aliases[Normalize(name)] {
		if p, ok := c.Part(r.ID); ok && p.Kind == kind && (variant == "" || strings.EqualFold(p.Variant, variant)) {
			exact = append(exact, p)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	for _, r := range c.Find(name) {
		if p, ok := c.Part(r.ID); ok && p.Kind == kind && (variant == "" || strings.EqualFold(p.Variant, variant)) {
			all = append(all, p)
		}
	}
	if len(all) == 1 {
		return all[0], nil
	}
	return nil, all
}
