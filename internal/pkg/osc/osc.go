package osc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/openSUSE/osc-mcp/internal/pkg/config"
	keyring "github.com/ppacher/go-dbus-keyring"
)

type OSCCredentials struct {
	Name      string
	Passwd    string
	Apiaddr   string
	SessionId string
	TempDir   string
}

// GetCredentials reads the osc configuration, determines the api url and
// returns the stored credentials.
// It will try to read ~/.oscrc.
// If use_keyring is set to 1 in the general section, it will try to read the
// password from the keyring. Otherwise it will use the pass value from the
// config file.
func GetCredentials(tempDir string) (creds OSCCredentials, err error) {
	creds.SessionId, err = generateRandomString(12)
	if err != nil {
		err = fmt.Errorf("failed to generate random string: %w", err)
		return
	}

	if tempDir != "" {
		creds.TempDir = tempDir
	} else {
		creds.TempDir = filepath.Join(os.TempDir(), "osc-mcp-"+creds.SessionId)
		if err = os.MkdirAll(creds.TempDir, 0755); err != nil {
			err = fmt.Errorf("failed to create temporary directory: %w", err)
			return
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		err = fmt.Errorf("could not get user home directory: %w", err)
		return
	}

	cfg := config.NewConfig()
	configPath := filepath.Join(home, ".oscrc")
	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		configPath = ".oscrc"
		if _, err = os.Stat(configPath); os.IsNotExist(err) {
			err = fmt.Errorf(".oscrc not found in home directory or current directory")
			return
		}
	}

	if err = cfg.Load(configPath); err != nil {
		err = fmt.Errorf("error loading config file: %w", err)
		return
	}

	apiurl := cfg.GetString("general", "apiurl")
	if apiurl == "" {
		err = fmt.Errorf("apiurl not set in general section of .oscrc")
		return
	}
	creds.Apiaddr = apiurl
	creds.Apiaddr = strings.TrimPrefix(creds.Apiaddr, "https://")
	creds.Apiaddr = strings.TrimPrefix(creds.Apiaddr, "http://")

	useKeyring := cfg.GetBool("general", "use_keyring")

	var keyringCreds OSCCredentials
	if useKeyring {
		keyringCreds, err = useKeyringCreds(creds.Apiaddr)
		if err != nil {
			return
		}
		creds.Name = keyringCreds.Name
		creds.Passwd = keyringCreds.Passwd
		return
	}

	user := cfg.GetString(apiurl, "user")
	pass := cfg.GetString(apiurl, "pass")

	if user == "" {
		err = fmt.Errorf("user not set for apiurl %s in .oscrc", apiurl)
		return
	}

	creds.Name = user
	creds.Passwd = pass

	return
}

// needed for session id
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func useKeyringCreds(apiAddr string) (cred OSCCredentials, err error) {
	cred.SessionId, err = generateRandomString(12)
	if err != nil {
		return cred, fmt.Errorf("failed to generate random string: %w", err)
	}
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

	login, err := secrets.GetCollection("Login")
	if err != nil {
		return cred, fmt.Errorf("failed to get Login collection: %w", err)
	}

	items, err := login.SearchItems(map[string]string{"service": apiAddr})
	if err != nil {
		return cred, fmt.Errorf("failed to search for items in keyring: %w", err)
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
	}

	return cred, nil
}
