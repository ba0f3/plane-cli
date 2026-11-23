package output

import (
	"encoding/json"
	"io"
	"os"

	"github.com/fatih/color"
	"gopkg.in/yaml.v3"
)

type Formatter struct {
	Format  string
	NoColor bool
	Wide    bool
}

func NewFormatter(format string, noColor bool) *Formatter {
	if noColor {
		color.NoColor = true
	}

	return &Formatter{
		Format:  NormalizeFormat(format),
		NoColor: noColor,
	}
}

func (f *Formatter) Print(data interface{}) error {
	return f.Fprint(os.Stdout, data)
}

// Fprint writes the formatted payload to the given writer. It applies the same
// normalization as Print, so structured output keeps snake_case keys in both
// JSON and YAML.
func (f *Formatter) Fprint(w io.Writer, data interface{}) error {
	format := NormalizeFormat(f.Format)
	if err := ValidateFormat(format); err != nil {
		return err
	}

	switch format {
	case "json":
		return f.writeJSON(w, data)
	case "yaml":
		return f.writeYAML(w, data)
	}

	return nil
}

func (f *Formatter) writeJSON(w io.Writer, data interface{}) error {
	normalized, err := normalizeStructuredOutput(data)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(normalized)
}

func (f *Formatter) writeYAML(w io.Writer, data interface{}) error {
	normalized, err := normalizeStructuredOutput(data)
	if err != nil {
		return err
	}

	encoder := yaml.NewEncoder(w)
	defer func() {
		_ = encoder.Close()
	}()
	return encoder.Encode(normalized)
}

func Success(msg string) {
	color.Green("✓ %s", msg)
}

func Error(msg string) {
	color.Red("✗ %s", msg)
}

func Warning(msg string) {
	color.Yellow("⚠ %s", msg)
}

func Info(msg string) {
	color.Cyan("ℹ %s", msg)
}
