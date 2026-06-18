// Package output renders command results as either an aligned text table or
// stable JSON. JSON field names are treated as a stable API (issue #1).
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// JSON writes v as indented JSON followed by a newline.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Table writes a header row and data rows as a tab-aligned table. Empty cells
// are rendered as a single dash for readability.
func Table(w io.Writer, header []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, strings.Join(header, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, c := range row {
			if c == "" {
				c = "-"
			}
			cells[i] = c
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}
