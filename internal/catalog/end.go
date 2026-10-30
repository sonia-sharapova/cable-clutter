package catalog

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// End is one end of a cable or adapter: a connector, its gender, and how many
// plugs of it there are (three for yellow/white/red RCA).
type End struct {
	Connector string
	Gender    string // "m" or "f"
	Count     int
}

// ParseEnd reads the "connector[*count]:gender" form, e.g. "rca*3:m".
// The connector is returned as written; resolving aliases is the caller's job.
func ParseEnd(s string) (End, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	conn, gender, ok := strings.Cut(s, ":")
	if !ok || conn == "" {
		return End{}, fmt.Errorf("end %q: want connector:gender, e.g. usb-c:m", s)
	}
	if gender != "m" && gender != "f" {
		return End{}, fmt.Errorf("end %q: gender must be m or f", s)
	}
	e := End{Connector: conn, Gender: gender, Count: 1}
	if name, n, ok := strings.Cut(conn, "*"); ok {
		count, err := strconv.Atoi(n)
		if err != nil || count < 1 {
			return End{}, fmt.Errorf("end %q: bad plug count %q", s, n)
		}
		e.Connector, e.Count = name, count
	}
	return e, nil
}

// MarshalText writes an end in its "connector[*count]:gender" form.
func (e End) MarshalText() ([]byte, error) { return []byte(e.String()), nil }

func (e End) String() string {
	if e.Count > 1 {
		return fmt.Sprintf("%s*%d:%s", e.Connector, e.Count, e.Gender)
	}
	return e.Connector + ":" + e.Gender
}

// Key is the canonical hashmap key for a cable or adapter type: the ends
// sorted and joined with "|", plus "#variant" when two types share the same
// ends (e.g. power supplies of different voltages). End order never matters.
func Key(ends []End, variant string) string {
	parts := make([]string, len(ends))
	for i, e := range ends {
		parts[i] = e.String()
	}
	sort.Strings(parts)
	k := strings.Join(parts, "|")
	if variant != "" {
		k += "#" + strings.ToLower(variant)
	}
	return k
}
