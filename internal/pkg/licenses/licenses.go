package licenses

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type License struct {
	LicenseID string `json:"licenseId"`
}

type LicenseList struct {
	Licenses []License `json:"licenses"`
}

var licensesJson []byte

func SetLicensesJson(data []byte) {
	licensesJson = data
}

func readLicenses() (LicenseList, error) {
	var licenseList LicenseList
	var jsonFile []byte
	var err error

	configPaths := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		configPaths = append(configPaths, filepath.Join(home, ".config", "osc-mcp", "licenses.json"))
	} else {
		slog.Warn("could not get user home directory, skipping user config", "err", err)
	}
	configPaths = append(configPaths, "/etc/osc-mcp/licenses.json", "/usr/etc/osc-mcp/licenses.json")

	var found bool
	for _, configPath := range configPaths {
		if _, err := os.Stat(configPath); err == nil {
			jsonFile, err = os.ReadFile(configPath)
			if err != nil {
				return LicenseList{}, fmt.Errorf("failed to read %s: %w", configPath, err)
			}
			slog.Debug("using licenses from", "path", configPath)
			found = true
			break
		}
	}

	if !found {
		slog.Debug("using embedded licenses")
		jsonFile = licensesJson
	}

	err = json.Unmarshal(jsonFile, &licenseList)
	if err != nil {
		return LicenseList{}, fmt.Errorf("failed to unmarshal licenses.json: %w", err)
	}
	return licenseList, nil
}

func GetLicenseIdentifiers(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	licenseList, err := readLicenses()
	if err != nil {
		return nil, err
	}

	var licenseIDs []string
	for _, license := range licenseList.Licenses {
		licenseIDs = append(licenseIDs, license.LicenseID)
	}

	data, err := json.Marshal(licenseIDs)
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      "mcp:licenses",
				Text:     string(data),
				MIMEType: "application/json",
			},
		},
	}, nil
}
