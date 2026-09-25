package auth

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	token     string
	apiHost   string
	workspace string
)

var AuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
	Long:  `Manage authentication with Plane.`,
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Plane",
	Long: `Authenticate with Plane using an API key.

You can generate an API key from your Plane workspace settings:
1. Go to Profile Settings → Personal Access Tokens
2. Click "Add personal access token"
3. Copy the generated token

Example:
  plane auth login
  plane auth login --token YOUR_API_KEY --workspace my-workspace`,
	RunE: runLogin,
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Logout from Plane",
	Long:  `Remove stored credentials and configuration.`,
	RunE:  runLogout,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check authentication status",
	Long:  `Check if you're authenticated and show current configuration.`,
	RunE:  runStatus,
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current user information",
	Long:  `Display information about the currently authenticated user.`,
	RunE:  runWhoami,
}

func init() {
	AuthCmd.AddCommand(loginCmd)
	AuthCmd.AddCommand(logoutCmd)
	AuthCmd.AddCommand(statusCmd)
	AuthCmd.AddCommand(whoamiCmd)

	loginCmd.Flags().StringVar(&token, "token", "", "API key (will prompt if not provided)")
	loginCmd.Flags().StringVar(&apiHost, "api-host", "", "Plane API host URL (will prompt if not provided)")
	loginCmd.Flags().StringVar(&workspace, "workspace", "", "Default workspace slug")
}

func runLogin(cmd *cobra.Command, args []string) error {
	if token == "" {
		prompt := &survey.Password{
			Message: "Enter your Plane API key:",
			Help:    "PAT, workspace access token (WSAT), or instance access token (IAT)",
		}
		if err := survey.AskOne(prompt, &token); err != nil {
			return err
		}
	}
	if token == "" {
		return fmt.Errorf("API key is required")
	}

	if apiHost == "" {
		prompt := &survey.Input{
			Message: "Enter your Plane API host:",
			Default: config.DefaultAPIHost,
			Help:    "The URL of your Plane instance (e.g. https://work.example.com)",
		}
		if err := survey.AskOne(prompt, &apiHost); err != nil {
			return err
		}
	}

	if err := config.SetAPIKey(token); err != nil {
		return fmt.Errorf("failed to save API key: %w", err)
	}
	if err := config.InitConfig(); err != nil {
		_ = config.DeleteAPIKey()
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	config.Cfg.APIHost = apiHost
	config.Cfg.DefaultWorkspace = workspace

	client, err := api.NewClientNoWorkspace()
	if err != nil {
		_ = config.DeleteAPIKey()
		return fmt.Errorf("failed to create API client: %w", err)
	}

	ctx, ctxErr := client.GetAuthContext()
	if ctxErr == nil {
		switch ctx.ScopeLevel {
		case "workspace":
			if workspace == "" && ctx.Workspace != nil {
				config.Cfg.DefaultWorkspace = ctx.Workspace.Slug
			}
		case "instance":
			// Intentionally keep the workspace empty unless explicitly provided.
		}
		if _, err := client.ListWorkspaces(); err != nil {
			_ = config.DeleteAPIKey()
			return fmt.Errorf("workspace discovery failed: %w", err)
		}
	} else {
		// Compatibility fallback for upstream Plane instances without auth/context.
		if workspace == "" {
			_ = config.DeleteAPIKey()
			return fmt.Errorf("server does not expose service-token discovery and no workspace was supplied: %w", ctxErr)
		}
		client.SetWorkspace(workspace)
		if _, err := client.GetUserInfo(); err != nil {
			_ = config.DeleteAPIKey()
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := client.ListProjects(); err != nil {
			_ = config.DeleteAPIKey()
			return fmt.Errorf("workspace validation failed: %w", err)
		}
	}

	if err := config.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if ctx != nil {
		if config.Cfg.DefaultWorkspace != "" {
			output.Success(fmt.Sprintf("Authenticated as %s/%s; default workspace '%s'", ctx.PrincipalType, ctx.ScopeLevel, config.Cfg.DefaultWorkspace))
		} else {
			output.Success(fmt.Sprintf("Authenticated as %s/%s; no default workspace (instance-wide reads enabled)", ctx.PrincipalType, ctx.ScopeLevel))
		}
	} else {
		output.Success(fmt.Sprintf("Successfully authenticated with workspace '%s'", config.Cfg.DefaultWorkspace))
	}
	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	if err := config.DeleteAPIKey(); err != nil {
		return fmt.Errorf("failed to remove API key: %w", err)
	}

	output.Success("Successfully logged out")
	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	apiKey, err := config.GetAPIKey()
	if err != nil || apiKey == "" {
		output.Error("Not authenticated")
		output.Info("Run 'plane auth login' to authenticate")
		return nil
	}

	if err := config.InitConfig(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("Authentication Status: ✓ Authenticated")
	fmt.Printf("Workspace: %s\n", config.Cfg.DefaultWorkspace)
	fmt.Printf("API Host: %s\n", config.Cfg.APIHost)
	fmt.Printf("Default Project: %s\n", config.Cfg.DefaultProject)
	fmt.Printf("Output Format: %s\n", config.Cfg.OutputFormat)

	return nil
}

func runWhoami(cmd *cobra.Command, args []string) error {
	if err := config.InitConfig(); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}

	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return err
	}

	// Get user info from the API
	user, err := client.GetUserInfo()
	if err != nil {
		return fmt.Errorf("failed to get user info: %w", err)
	}

	fmt.Printf("User: %s %s\n", user.FirstName, user.LastName)
	fmt.Printf("Email: %s\n", user.Email)
	fmt.Printf("Display Name: %s\n", user.DisplayName)
	fmt.Printf("Workspace: %s\n", client.Workspace)

	return nil
}
