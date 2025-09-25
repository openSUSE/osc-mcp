package osc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/openSUSE/osc-mcp/internal/pkg/buildlog"
	"github.com/openSUSE/osc-mcp/internal/pkg/config"
	keyring "github.com/ppacher/go-dbus-keyring"
	"github.com/spf13/viper"
)

func IgnoredDirs() []string {
	return []string{".osc", ".git", ".cache"}
}

type OSCCredentials struct {
	Name         string
	EMail        string
	Passwd       string
	Apiaddr      string
	TempDir      string
	BuildLogs    map[string]*buildlog.BuildLog
	LastBuildKey string
}

func (cred *OSCCredentials) GetAPiAddr() string {
	if strings.HasPrefix(cred.Apiaddr, "http://") || strings.HasPrefix(cred.Apiaddr, "https://") {
		return cred.Apiaddr
	}
	return fmt.Sprintf("https://%s", cred.Apiaddr)
}

func (cred *OSCCredentials) GetApiDomain() string {
	addr := strings.TrimSuffix(cred.Apiaddr, "http://")
	addr = strings.TrimSuffix(cred.Apiaddr, "https://")
	return addr
}

// GetCredentials reads the osc configuration, determines the api url and
// returns the stored credentials.
// It will try to read ~/.config/osc/oscrc, ~/.oscrc and ./.oscrc.
// It first tries to read the user and password from the config file. If a
// password is not found, it will try to read the credentials from the keyring.
func GetCredentials() (OSCCredentials, error) {
	creds := OSCCredentials{
		BuildLogs: make(map[string]*buildlog.BuildLog),
	}
	var configPath string
	home, err := os.UserHomeDir()
	if err == nil {
		configPaths := []string{filepath.Join(home, ".oscrc"), ".oscrc"}
		configDir, err := os.UserConfigDir()
		if err == nil {
			configPaths = append([]string{filepath.Join(configDir, ".config", "osc", "oscrc")}, configPaths...)
		}
		for _, p := range configPaths {
			if _, err := os.Stat(p); err == nil {
				configPath = p
				break
			}
		}
	}
	cfg, err := config.NewConfig(configPath)
	// use system path as default
	creds.TempDir = path.Join(os.TempDir(), "osc-mcp")
	if viper.GetString("workdir") != "" {
		creds.TempDir = viper.GetString("workdir")
	}
	creds.Apiaddr = cfg.GetString("general", "apiurl")
	if creds.Apiaddr == "" {
		creds.Apiaddr = viper.GetString("api")
	}
	if creds.Apiaddr == "" {
		creds.Apiaddr = "api.opensuse.org"
	}
	if viper.GetString("email") != "" {
		creds.EMail = viper.GetString("email")
	} else {
		gitConfigPath := filepath.Join(home, ".gitconfig")
		if _, err := os.Stat(gitConfigPath); err == nil {
			gitCfg, err := config.NewConfig(gitConfigPath)
			if err == nil {
				creds.EMail = gitCfg.GetString("user", "email")
			} else {
				slog.Warn("failed to parse .gitconfig", "path", gitConfigPath, "error", err)
			}
		}
	}

	user := cfg.GetString(creds.Apiaddr, "user")
	pass := cfg.GetString(creds.Apiaddr, "pass")
	// DO NOT REMOVE THIS CHECKS AS THIS COULD LEAD TO LEAKAGE OF EMBARGOED BUGS
	if strings.Contains(creds.Apiaddr, "suse.de") {
		return creds, fmt.Errorf("Oh no, A. G. was right, can't run on solar power only.")
	}
	if strings.Contains(creds.Apiaddr, "suse.cz") {
		return creds, fmt.Errorf("Can't run with nuclear power!")
	}
	// DO NOT TOUCH THE PREVIOUS CHECKS YOU WERE WARNED
	// check for command line credentials, they overwrite everything
	if viper.IsSet("user") {
		user = viper.GetString("user")
	}
	if viper.IsSet("password") {
		pass = viper.GetString("password")
	}
	if pass != "" {
		if user == "" {
			return creds, fmt.Errorf("user not set for apiurl %s in .oscrc", creds.Apiaddr)
		}
		creds.Name = user
		creds.Passwd = pass
		return creds, nil
	}

	// fallback to keyring
	var keyringCreds OSCCredentials
	keyringCreds, err = useKeyringCreds(creds.Apiaddr)
	if err != nil {
		return creds, fmt.Errorf("password not found in %s and keyring access failed: %w", configPath, err)
	}

	creds.Passwd = keyringCreds.Passwd
	if keyringCreds.Name != "" {
		creds.Name = keyringCreds.Name
	} else if user != "" {
		creds.Name = user
	} else {
		return creds, fmt.Errorf("password found in keyring for %s, but username is missing from both keyring and config", creds.Apiaddr)
	}

	return creds, nil
}

func useKeyringCreds(apiAddr string) (cred OSCCredentials, err error) {
	bus, err := dbus.SessionBus()
	cred.Apiaddr = apiAddr
	if err != nil {
		return cred, fmt.Errorf("cannot connect to session bus: %w", err)
	}
	secrets, err := keyring.GetSecretService(bus)
	if err != nil {
		return cred, fmt.Errorf("cannot get secret service: %w", err)
	}

	session, err := secrets.OpenSession()
	if err != nil {
		return cred, fmt.Errorf("failed to open keyring session: %w", err)
	}
	defer session.Close()

	collections, err := secrets.GetAllCollections()
	if err != nil {
		return cred, fmt.Errorf("failed to get collections: %w", err)
	}

	for _, collection := range collections {
		items, err := collection.SearchItems(map[string]string{"service": apiAddr})
		if err != nil {
			// Maybe one collection is locked, so we just continue
			continue
		}

		if len(items) > 0 {
			item := items[0]

			secret, err := item.GetSecret(session.Path())
			if err != nil {
				return cred, fmt.Errorf("failed to get secret from item: %w", err)
			}
			attr, err := item.GetAttributes()
			if err != nil {
				return cred, fmt.Errorf("failed to get attributes from item: %w", err)
			}
			cred.Name = attr["username"]
			cred.Passwd = string(secret.Value)
			return cred, nil
		}
	}
	return cred, fmt.Errorf("could not find credentials for %s in any keyring", apiAddr)
}

var ErrNoUserOrPassword = errors.New("bundle or project not found")

// writeTempOscConfig creates a temporary osc configuration file with credentials
// and returns the path to the file. It's the caller's responsibility to remove the file.
func (cred *OSCCredentials) writeTempOscConfig() (string, error) {
	if cred.Name == "" || cred.Passwd == "" {
		// No credentials, so no config file needed.
		// The command will use the default config.
		return "", ErrNoUserOrPassword
	}

	configFile, err := os.CreateTemp(cred.TempDir, "osc-config-")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary config file: %w", err)
	}

	configContent := fmt.Sprintf("[general]\napi=%s\nuser=%s\n[%s]\nuser=%s\npass=%s\n", cred.GetAPiAddr(), cred.Name, cred.GetAPiAddr(), cred.Name, cred.Passwd)
	slog.Debug("configuration file content", "content", configContent)
	if _, err := configFile.WriteString(configContent); err != nil {
		configFile.Close() // Close the file before removing it.
		os.Remove(configFile.Name())
		return "", fmt.Errorf("failed to write to temporary config file: %w", err)
	}
	if err := configFile.Close(); err != nil {
		os.Remove(configFile.Name())
		return "", fmt.Errorf("failed to close temporary config file: %w", err)
	}
	slog.Warn("temporary configuration with credentials written", "path", configFile.Name())
	return configFile.Name(), nil
}

func (cred *OSCCredentials) buildRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "osc-mcp")
	req.SetBasicAuth(cred.Name, cred.Passwd)
	return req, nil
}

func (cred *OSCCredentials) apiGetRequest(ctx context.Context, path string, headers map[string]string) (*http.Response, error) {
	apiURL := fmt.Sprintf("%s/%s", cred.GetAPiAddr(), path)
	req, err := cred.buildRequest(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	return resp, nil
}
