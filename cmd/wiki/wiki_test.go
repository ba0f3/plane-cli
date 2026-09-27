package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func payloadTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&pageName, "name", "", "")
	cmd.Flags().StringVar(&pageMarkdown, "markdown", "", "")
	cmd.Flags().StringVar(&pageFile, "file", "", "")
	cmd.Flags().StringVar(&pageHTML, "html", "", "")
	cmd.Flags().StringVar(&pageParent, "parent", "", "")
	cmd.Flags().StringVar(&pageColor, "color", "", "")
	cmd.Flags().Float64Var(&pageSort, "sort-order", 0, "")
	return cmd
}

func resetPayloadGlobals() {
	pageName = ""
	pageMarkdown = ""
	pageFile = ""
	pageHTML = ""
	pageParent = ""
	pageColor = ""
	pageSort = 0
}

func TestWritePayloadReadsMarkdownFileUnchanged(t *testing.T) {
	resetPayloadGlobals()
	t.Cleanup(resetPayloadGlobals)

	markdown := "# Runbook\n\n- one\n- two\n\n```sh\necho ok\n```\n"
	path := filepath.Join(t.TempDir(), "runbook.md")
	if err := os.WriteFile(path, []byte(markdown), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := payloadTestCommand()
	if err := cmd.Flags().Set("name", "Runbook"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("file", path); err != nil {
		t.Fatal(err)
	}

	payload, err := writePayload(cmd, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := payload["description_markdown"]; got != markdown {
		t.Fatalf("markdown changed in transit: got %#v want %#v", got, markdown)
	}
	if _, exists := payload["description_html"]; exists {
		t.Fatal("file import must not populate description_html")
	}
}

func TestWritePayloadMarkdownFlagIsRaw(t *testing.T) {
	resetPayloadGlobals()
	t.Cleanup(resetPayloadGlobals)

	cmd := payloadTestCommand()
	if err := cmd.Flags().Set("markdown", "**raw**"); err != nil {
		t.Fatal(err)
	}

	payload, err := writePayload(cmd, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := payload["description_markdown"]; got != "**raw**" {
		t.Fatalf("unexpected markdown payload: %#v", got)
	}
}

func TestWritePayloadRejectsMultipleContentInputs(t *testing.T) {
	resetPayloadGlobals()
	t.Cleanup(resetPayloadGlobals)

	cmd := payloadTestCommand()
	if err := cmd.Flags().Set("markdown", "# Markdown"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("html", "<p>HTML</p>"); err != nil {
		t.Fatal(err)
	}

	_, err := writePayload(cmd, false)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestWritePayloadRejectsNonUTF8File(t *testing.T) {
	resetPayloadGlobals()
	t.Cleanup(resetPayloadGlobals)

	path := filepath.Join(t.TempDir(), "bad.md")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0xfd}, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := payloadTestCommand()
	if err := cmd.Flags().Set("file", path); err != nil {
		t.Fatal(err)
	}

	_, err := writePayload(cmd, false)
	if err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("expected UTF-8 validation error, got %v", err)
	}
}
