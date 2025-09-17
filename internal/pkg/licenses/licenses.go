package licenses

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type License struct {
	LicenseID string `json:"licenseId"`
}

type LicenseList struct {
	Licenses []License `json:"licenses"`
}

func GetLicenseIdentifiers(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	jsonFile, err := os.Open("data/licenses.json")
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return nil, err
	}

	var licenseList LicenseList
	if err := json.Unmarshal(byteValue, &licenseList); err != nil {
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
