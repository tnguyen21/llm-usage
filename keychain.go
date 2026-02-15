package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func loadToken() (string, string, error) {
	// env var override first
	if tok := os.Getenv("CLAUDE_OAUTH_TOKEN"); tok != "" {
		return tok, "", nil
	}

	if runtime.GOOS != "darwin" {
		return "", "", fmt.Errorf("CLAUDE_OAUTH_TOKEN must be set (Keychain auto-detection is macOS-only)")
	}

	securityPath := "/usr/bin/security"
	if _, err := os.Stat(securityPath); err != nil {
		// Fallback for unusual setups; still prefer an absolute path when possible.
		if lp, lookErr := exec.LookPath("security"); lookErr == nil && filepath.IsAbs(lp) {
			securityPath = lp
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(
		ctx,
		securityPath, "find-generic-password",
		"-s", "Claude Code-credentials",
		"-w",
	).Output()
	if err != nil {
		return "", "", fmt.Errorf("no Claude Code credentials found in Keychain")
	}

	var creds KeychainCredentials
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &creds); err != nil {
		return "", "", fmt.Errorf("failed to parse Keychain credentials: %w", err)
	}

	if creds.ClaudeAiOauth == nil || creds.ClaudeAiOauth.AccessToken == "" {
		return "", "", fmt.Errorf("no OAuth token in Keychain credentials")
	}

	return creds.ClaudeAiOauth.AccessToken, creds.ClaudeAiOauth.SubscriptionType, nil
}
