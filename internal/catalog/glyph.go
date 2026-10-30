package catalog

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxGlyphWidth keeps two glyphs, a wire and labels within 80 columns.
const MaxGlyphWidth = 14

// Glyph is a connector's outline. '#' in Art marks the contact area: Fill on
// a male end, blank on a female one. Female, if set, replaces Art for
// female ends.
type Glyph struct {
	Art    []string
	Female []string
	Fill   rune
}

// Lines draws the glyph for one gender, every line padded to the same width.
func (g Glyph) Lines(gender string) []string {
	art, fill := g.Art, " "
	if gender == "m" {
		fill = string(g.Fill)
	} else if g.Female != nil {
		art = g.Female
	}
	w := width(art)
	out := make([]string, len(art))
	for i, l := range art {
		out[i] = strings.ReplaceAll(l, "#", fill) + strings.Repeat(" ", w-len(l))
	}
	return out
}

func width(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, len(l))
	}
	return w
}

// Glyph returns the outline for a connector: its own, else its family's,
// else the default.
func (c *Catalog) Glyph(connector string) Glyph {
	if g, ok := c.glyphs[connector]; ok {
		return g
	}
	if cn, ok := c.Connectors[connector]; ok {
		if g, ok := c.glyphs["family:"+cn.Family]; ok {
			return g
		}
	}
	return c.glyphs["default"]
}

func (c *Catalog) addGlyphs(raw map[string]rawGlyph) error {
	families := map[string]bool{}
	for _, cn := range c.Connectors {
		families[cn.Family] = true
	}
	var errs []error
	for key, r := range raw {
		fam, isFamily := strings.CutPrefix(key, "family:")
		_, isConnector := c.Connectors[key]
		if key != "default" && !isConnector && !(isFamily && families[fam]) {
			errs = append(errs, fmt.Errorf("glyph %q: not a connector, family:<name> or default", key))
			continue
		}
		if utf8.RuneCountInString(r.Fill) != 1 {
			errs = append(errs, fmt.Errorf("glyph %q: fill must be one character", key))
			continue
		}
		fill, _ := utf8.DecodeRuneInString(r.Fill)
		g := Glyph{Art: splitArt(r.Art), Fill: fill}
		if r.Female != "" {
			g.Female = splitArt(r.Female)
		}
		for _, art := range [][]string{g.Art, g.Female} {
			for _, l := range art {
				if strings.ContainsFunc(l, func(ch rune) bool { return ch > '~' || ch < ' ' }) {
					errs = append(errs, fmt.Errorf("glyph %q: art must be printable ASCII", key))
				}
			}
			if w := width(art); w > MaxGlyphWidth {
				errs = append(errs, fmt.Errorf("glyph %q: %d columns wide, max %d", key, w, MaxGlyphWidth))
			}
		}
		c.glyphs[key] = g
	}
	if _, ok := c.glyphs["default"]; !ok {
		errs = append(errs, errors.New(`glyphs: a "default" glyph is required`))
	}
	return errors.Join(errs...)
}

func splitArt(s string) []string { return strings.Split(strings.TrimRight(s, "\n"), "\n") }
