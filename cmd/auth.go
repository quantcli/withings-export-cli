package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/quantcli/withings-export-cli/internal/auth"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:          "auth",
	Short:        "Authentication commands",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SetOut(cmd.ErrOrStderr())
		_ = cmd.Help()
		return fmt.Errorf("subcommand required")
	},
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Withings via OAuth2 in your browser",
	Long: `Log in to Withings.

You need a Withings developer app — create one at https://developer.withings.com/.
Withings requires an HTTPS callback URL; the recommended workaround is to register
something like:

  https://redirectmeto.com/http://localhost:8128/oauth/authorize

and set WITHINGS_CALLBACK_URL to the exact same value. The CLI will bind a local
server on the embedded host:port and catch the redirect.

Credentials can be supplied via WITHINGS_CLIENT_ID, WITHINGS_CLIENT_SECRET, and
WITHINGS_CALLBACK_URL environment variables, or you will be prompted for the
client id and secret.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clientID, clientSecret := auth.CredentialsFromEnv()

		reader := bufio.NewReader(os.Stdin)
		if clientID == "" {
			fmt.Print("Withings Client ID: ")
			line, _ := reader.ReadString('\n')
			clientID = strings.TrimSpace(line)
		}
		if clientSecret == "" {
			fmt.Print("Withings Client Secret: ")
			line, _ := reader.ReadString('\n')
			clientSecret = strings.TrimSpace(line)
		}
		if clientID == "" || clientSecret == "" {
			return fmt.Errorf("client ID and secret are required")
		}

		callbackURL := auth.CallbackURLFromEnv()
		if err := auth.Login(cmd.Context(), clientID, clientSecret, callbackURL); err != nil {
			return err
		}
		fmt.Println("Logged in. Tokens saved to ~/.config/withings-export/auth.json")
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored auth tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.Logout(); err != nil {
			return err
		}
		fmt.Println("Logged out.")
		return nil
	},
}

var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Force a token refresh and report status",
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := auth.GetToken()
		if err != nil {
			return err
		}
		fmt.Printf("Token valid: %s...\n", token[:min(20, len(token))])
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print one-line auth readiness state and exit 0 if usable",
	Long: `Print a one-line summary of whether the CLI has a usable token. Exit 0
if a saved token is present and not yet expired, 1 otherwise.

This is a local check — no network call and no refresh is attempted, even
when the saved token is expired. Use 'auth refresh' (or any export
subcommand) to actually refresh.

Per the quantcli shared contract:
https://github.com/quantcli/common/blob/main/CONTRACT.md#5-auth`,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := auth.Load()
		if err != nil {
			return fmt.Errorf("not logged in — run: withings-export auth login")
		}
		exp := store.ExpiresAt.Local().Format(time.RFC3339)
		if time.Now().After(store.ExpiresAt) {
			return fmt.Errorf("token expired %s — run: withings-export auth refresh", exp)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "logged in as user %s (token expires %s)\n", store.UserID, exp)
		return nil
	},
}

func init() {
	authCmd.AddCommand(loginCmd)
	authCmd.AddCommand(logoutCmd)
	authCmd.AddCommand(refreshCmd)
	authCmd.AddCommand(statusCmd)
}
