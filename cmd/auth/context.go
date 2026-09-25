package auth

import (
	"fmt"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Show API principal and service-token scope",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.InitConfig(); err != nil {
			return fmt.Errorf("failed to initialize config: %w", err)
		}
		client, err := api.NewClientNoWorkspace()
		if err != nil {
			return err
		}
		ctx, err := client.GetAuthContext()
		if err != nil {
			return err
		}
		formatter := output.NewFormatter(config.Cfg.OutputFormat, false)
		return formatter.Print(ctx)
	},
}

func init() {
	AuthCmd.AddCommand(contextCmd)
}
