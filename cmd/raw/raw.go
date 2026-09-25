package raw

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rohithmahesh3/plane-cli/internal/api"
	"github.com/spf13/cobra"
)

var rawData string

var RawCmd = &cobra.Command{
	Use:   "raw <METHOD> <PATH>",
	Short: "Call a Plane API endpoint directly",
	Long:  "Escape hatch for automation. PATH is relative to /api/v1, for example /auth/context/.",
	Args:  cobra.ExactArgs(2),
	RunE:  runRaw,
}

func init() {
	RawCmd.Flags().StringVarP(&rawData, "data", "d", "", "JSON request body")
}

func runRaw(cmd *cobra.Command, args []string) error {
	method := strings.ToUpper(args[0])
	path := args[1]
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var body interface{}
	if rawData != "" {
		var decoded interface{}
		if err := json.Unmarshal([]byte(rawData), &decoded); err != nil {
			return fmt.Errorf("invalid --data JSON: %w", err)
		}
		body = decoded
	}

	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return err
	}
	req, err := client.NewRequest(method, path, body)
	if err != nil {
		return err
	}
	resp, err := client.DoRaw(req)
	if err != nil {
		return err
	}
	if len(resp) == 0 {
		return nil
	}

	var pretty interface{}
	if json.Unmarshal(resp, &pretty) == nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(pretty)
	}
	_, err = os.Stdout.Write(append(resp, '\n'))
	return err
}
