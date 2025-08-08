package osc

import (
	"fmt"
	"github.com/godbus/dbus/v5"
	keyring "github.com/ppacher/go-dbus-keyring"
)

type OSCCredentials struct {
	Name   string
	Passwd string
}

func UseKeyring(apiAddr string) (cred OSCCredentials, err error) {
	bus, err := dbus.SessionBus()
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
