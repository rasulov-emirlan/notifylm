package googleauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GetOAuth2Client returns an authenticated HTTP client using stored or new OAuth2 credentials.
// It loads credentials from credentialsPath, attempts to load a cached token from tokenPath,
// and falls back to an interactive OAuth flow if no valid token is found.
func GetOAuth2Client(ctx context.Context, credentialsPath, tokenPath string, scopes ...string) (*http.Client, error) {
	creds, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}

	cfg, err := google.ConfigFromJSON(creds, scopes...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	token, err := loadToken(tokenPath)
	if err != nil {
		token, err = authorize(ctx, cfg, tokenPath)
		if err != nil {
			return nil, fmt.Errorf("failed to authorize: %w", err)
		}
	}

	return cfg.Client(ctx, token), nil
}

func loadToken(path string) (*oauth2.Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

func authorize(ctx context.Context, cfg *oauth2.Config, tokenPath string) (*oauth2.Token, error) {
	authURL := cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Open this URL in your browser:\n%s\n\n", authURL)
	fmt.Print("Enter authorization code: ")

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return nil, fmt.Errorf("failed to read auth code: %w", err)
	}

	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	if err := saveToken(tokenPath, token); err != nil {
		fmt.Printf("Warning: failed to save token: %v\n", err)
	}

	return token, nil
}

func saveToken(path string, token *oauth2.Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
