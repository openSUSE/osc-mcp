package osc

import (
	"bytes"
	"compress/bzip2"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/openSUSE/osc-mcp/internal/pkg/config"
)

// obfuscatedCredentialsManager is the osc credentials manager that stores the
// password in the config file as base64-encoded bzip2 data.
const obfuscatedCredentialsManager = "osc.credentials.ObfuscatedConfigFileCredentialsManager"

// configPassword returns the password stored in the osc config section for an
// API URL, decoding it the way osc does.
//
// osc decodes the deprecated "passx" option whenever it is set, and decodes
// "pass" when the section's credentials_mgr_class is
// osc.credentials.ObfuscatedConfigFileCredentialsManager. Otherwise "pass" is
// plain text.
func configPassword(cfg *config.Config, section string) (string, error) {
	if passx := cfg.GetString(section, "passx"); passx != "" {
		pass, err := decodeObfuscatedPassword(passx)
		if err != nil {
			return "", fmt.Errorf("cannot decode passx for %s: %w", section, err)
		}
		return pass, nil
	}
	pass := cfg.GetString(section, "pass")
	if pass != "" && cfg.GetString(section, "credentials_mgr_class") == obfuscatedCredentialsManager {
		decoded, err := decodeObfuscatedPassword(pass)
		if err != nil {
			return "", fmt.Errorf("cannot decode obfuscated password for %s: %w", section, err)
		}
		return decoded, nil
	}
	return pass, nil
}

// decodeObfuscatedPassword reverses osc's password obfuscation: base64, then
// bzip2 (osc.credentials.ObfuscatedConfigFileCredentialsManager.decode_password).
func decodeObfuscatedPassword(obfuscated string) (string, error) {
	compressed, err := base64.StdEncoding.DecodeString(obfuscated)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	plain, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(compressed)))
	if err != nil {
		return "", fmt.Errorf("invalid bzip2 data: %w", err)
	}
	return string(plain), nil
}
