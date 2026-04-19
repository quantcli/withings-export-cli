package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/quantcli/withings-export-cli/internal/auth"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Withings via OAuth2 in your browser",
	Long: `Log in to Withings.

You need a Withings developer app — create one at https://developer.withings.com/
and set the callback URL to http://127.0.0.1 (any port).

Credentials can be supplied via the WITHINGS_CLIENT_ID and WITHINGS_CLIENT_SECRET
environment variables, or you will be prompted for them.`,
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

		if err := auth.Login(cmd.Context(), clientID, clientSecret); err != nil {
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

func init() {
	authCmd.AddCommand(loginCmd)
	authCmd.AddCommand(logoutCmd)
	authCmd.AddCommand(refreshCmd)
}
