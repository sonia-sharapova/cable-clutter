// Package outline draws a cable or adapter as the outlines of its ends, side
// by side, joined by its wire:
//
//	USB-A (M)                 USB-C (M)
//	.----------.              .--------.
//	| ======== |~~~~~~~~~~~~~~( ====== )
//	'----------'              '--------'
//
// Parts with more than two ends (hubs, splitters) branch from one trunk.
package outline

import (
	"fmt"
	"strings"

	"cable-clutter/internal/catalog"
)

const wireLen = 14

// Render draws p. color is an ANSI sequence applied to the outlines (not the
// labels), or "" for none.
func Render(c *catalog.Catalog, p *catalog.Part, color string) string {
	left := 0 // the plug side: first male end, else the first end
	for i, e := range p.Ends {
		if e.Gender == "m" {
			left = i
			break
		}
	}
	var rights []block
	for i, e := range p.Ends {
		if i == left {
			continue
		}
		// Several tall plugs (a splitter's two sockets) branch like separate
		// ends; one-line plugs such as RCA stay stacked with their labels.
		if e.Count > 1 && len(c.Glyph(e.Connector).Art) > 1 {
			single := e
			single.Count = 1
			for range e.Count {
				rights = append(rights, newBlock(c, single, p.Signals))
			}
			continue
		}
		rights = append(rights, newBlock(c, e, p.Signals))
	}
	l := newBlock(c, p.Ends[left], p.Signals)

	// Stack the right-hand blocks with a blank row between, then line the
	// left block's wire row up with the first right block's.
	var rightRows []string
	var wireRows []int
	var rightGlyph []bool
	for i, b := range rights {
		if i > 0 {
			rightRows = append(rightRows, "")
			rightGlyph = append(rightGlyph, false)
		}
		wireRows = append(wireRows, len(rightRows)+b.mid)
		rightRows = append(rightRows, b.lines...)
		for j := range b.lines {
			rightGlyph = append(rightGlyph, j > 0)
		}
	}
	shift := wireRows[0] - l.mid // >0: left block starts lower
	leftTop, rightTop := max(shift, 0), max(-shift, 0)
	first, last := wireRows[0]+rightTop, wireRows[len(wireRows)-1]+rightTop
	branch := map[int]bool{}
	for _, r := range wireRows[1:] {
		branch[r+rightTop] = true
	}

	wire := "~"
	if p.Kind == catalog.KindAdapter {
		wire = "=" // rigid body, no cable
	}
	trunk := wireLen / 3
	height := max(leftTop+len(l.lines), rightTop+len(rightRows))
	var out strings.Builder
	for r := range height {
		var row strings.Builder
		if i := r - leftTop; i >= 0 && i < len(l.lines) {
			if r == first {
				// The wire leaves straight from the plug, not the label's edge.
				row.WriteString(paint(l.lines[i], color, true) + strings.Repeat(wire, l.width-len(l.lines[i])))
			} else {
				row.WriteString(paint(pad(l.lines[i], l.width), color, i > 0))
			}
		} else {
			row.WriteString(strings.Repeat(" ", l.width))
		}

		switch {
		case r == first:
			if len(rights) > 1 {
				row.WriteString(strings.Repeat(wire, trunk) + "+" + strings.Repeat(wire, wireLen-trunk-1))
			} else {
				row.WriteString(strings.Repeat(wire, wireLen))
			}
		case branch[r]:
			corner := "+"
			if r == last {
				corner = "'"
			}
			row.WriteString(strings.Repeat(" ", trunk) + corner + strings.Repeat(wire, wireLen-trunk-1))
		case r > first && r < last:
			row.WriteString(strings.Repeat(" ", trunk) + "|" + strings.Repeat(" ", wireLen-trunk-1))
		default:
			row.WriteString(strings.Repeat(" ", wireLen))
		}

		if i := r - rightTop; i >= 0 && i < len(rightRows) {
			l := rightRows[i]
			if r == first || branch[r] {
				// Run the wire up to the plug's first character.
				body := strings.TrimLeft(l, " ")
				row.WriteString(strings.Repeat(wire, len(l)-len(body)))
				l = body
			}
			row.WriteString(paint(l, color, rightGlyph[i]))
		}
		out.WriteString(strings.TrimRight(row.String(), " ") + "\n")
	}
	return out.String()
}

// block is one end: a label line, then the glyph (stacked once per plug).
type block struct {
	lines []string
	width int
	mid   int // the row the wire attaches to
}

func newBlock(c *catalog.Catalog, e catalog.End, signals []string) block {
	label := shortName(c, e.Connector)
	if e.Count > 1 {
		label += fmt.Sprintf(" x%d", e.Count)
	}
	label += " (" + strings.ToUpper(e.Gender) + ")"

	g := c.Glyph(e.Connector).Lines(e.Gender)
	names := plugNames(e, signals)
	b := block{lines: []string{label}}
	for i := range e.Count {
		for _, l := range g {
			if names != nil && len(g) == 1 {
				l += " " + names[i]
			}
			b.lines = append(b.lines, l)
		}
	}
	for _, l := range b.lines {
		b.width = max(b.width, len(l))
	}
	b.mid = 1 + (len(b.lines)-2)/2
	return b
}

// shortName is the connector's name without its parenthetical:
// "USB 3.0 Type-A (SuperSpeed, blue)" -> "USB 3.0 Type-A".
func shortName(c *catalog.Catalog, id string) string {
	cn, ok := c.Connectors[id]
	if !ok {
		return id
	}
	name, _, _ := strings.Cut(cn.Name, " (")
	return name
}

// plugNames labels each plug of a multi-plug RCA end by colour and job.
func plugNames(e catalog.End, signals []string) []string {
	if e.Connector != "rca" {
		return nil
	}
	has := func(s string) bool {
		for _, x := range signals {
			if x == s {
				return true
			}
		}
		return false
	}
	stereo := []string{"white   audio L", "red     audio R"}
	switch {
	case e.Count == 2:
		return stereo
	case e.Count == 3 && has("video.composite"):
		return append([]string{"yellow  video"}, stereo...)
	case e.Count == 5 && has("video.component"):
		return append([]string{"green   Y", "blue    Pb", "red     Pr"}, stereo...)
	}
	return nil
}

func pad(s string, w int) string { return s + strings.Repeat(" ", w-len(s)) }

func paint(s, color string, glyph bool) string {
	if color == "" || !glyph || strings.TrimSpace(s) == "" {
		return s
	}
	return color + s + Reset
}

// ANSI colours for item status.
const (
	Green = "\x1b[32m"
	Amber = "\x1b[33m"
	Red   = "\x1b[31m"
	Reset = "\x1b[0m"
)
