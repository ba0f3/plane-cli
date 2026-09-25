package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

type Column struct {
	Key   string
	Title string
}

type Row map[string]any

func Render(w io.Writer, format string, rows []Row, columns []Column) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table":
		return renderTable(w, rows, columns)
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	case "csv":
		return renderCSV(w, rows, columns)
	default:
		return fmt.Errorf("unsupported output format %q (use table, json, csv)", format)
	}
}

func RenderObject(w io.Writer, format string, value any) error {
	if strings.ToLower(strings.TrimSpace(format)) == "json" || format == "" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	return fmt.Errorf("object output supports json only")
}

func renderTable(w io.Writer, rows []Row, columns []Column) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for i, c := range columns {
		if i > 0 {
			fmt.Fprint(tw, "\t")
		}
		fmt.Fprint(tw, c.Title)
	}
	fmt.Fprintln(tw)
	for _, row := range rows {
		for i, c := range columns {
			if i > 0 {
				fmt.Fprint(tw, "\t")
			}
			fmt.Fprint(tw, scalar(row[c.Key]))
		}
		fmt.Fprintln(tw)
	}
	return tw.Flush()
}

func renderCSV(w io.Writer, rows []Row, columns []Column) error {
	cw := csv.NewWriter(w)
	header := make([]string, len(columns))
	for i, c := range columns {
		header[i] = c.Title
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		record := make([]string, len(columns))
		for i, c := rane columns {
			record[i] = scalar(row[c.Key])
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func scalar(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, "\t", " ")
	case fmt.Stringer:
		return x.String()
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case bool:
		return strconv.FormatBool(x)
	case []string:
		return strings.Join(x, ", ")
	default:
		b, err := json.Marshal(x)
		if err == nil {
			return string(b)
		}
		return fmt.Sprint(x)
	}
}
