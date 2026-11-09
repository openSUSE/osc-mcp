package osc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openSUSE/osc-mcp/internal/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// obfuscatedSecret is "opensuse-secret" as osc stores it with
// ObfuscatedConfigFileCredentialsManager:
// base64.b64encode(bz2.compress(b"opensuse-secret")).
const obfuscatedSecret = "QlpoOTFBWSZTWbnyCdoAAAORgAACCgHeACAAMQAwIAwlwYBxItMPxdyRThQkLnyCdoA="

func writeConfig(t *testing.T, content string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "oscrc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	cfg, err := config.NewConfig(path)
	require.NoError(t, err)
	return cfg
}

func TestDecodeObfuscatedPassword(t *testing.T) {
	pass, err := decodeObfuscatedPassword(obfuscatedSecret)
	require.NoError(t, err)
	assert.Equal(t, "opensuse-secret", pass)

	_, err = decodeObfuscatedPassword("not base64!")
	assert.Error(t, err)

	// Valid base64, but not bzip2 data.
	_, err = decodeObfuscatedPassword("b3BlbnN1c2Utc2VjcmV0")
	assert.Error(t, err)
}

func TestConfigPassword(t *testing.T) {
	const section = "https://api.opensuse.org"
	tests := []struct {
		name    string
		oscrc   string
		want    string
		wantErr bool
	}{
		{
			name:  "plain pass",
			oscrc: "[https://api.opensuse.org]\nuser=me\npass=plain-secret\n",
			want:  "plain-secret",
		},
		{
			name: "plain pass with the plaintext manager",
			oscrc: "[https://api.opensuse.org]\nuser=me\npass=plain-secret\n" +
				"credentials_mgr_class=osc.credentials.PlaintextConfigFileCredentialsManager\n",
			want: "plain-secret",
		},
		{
			name: "obfuscated pass",
			oscrc: "[https://api.opensuse.org]\nuser=me\npass=" + obfuscatedSecret + "\n" +
				"credentials_mgr_class=osc.credentials.ObfuscatedConfigFileCredentialsManager\n",
			want: "opensuse-secret",
		},
		{
			name:  "deprecated passx",
			oscrc: "[https://api.opensuse.org]\nuser=me\npassx=" + obfuscatedSecret + "\n",
			want:  "opensuse-secret",
		},
		{
			name:  "passx wins over pass",
			oscrc: "[https://api.opensuse.org]\nuser=me\npass=plain-secret\npassx=" + obfuscatedSecret + "\n",
			want:  "opensuse-secret",
		},
		{
			name:  "no password",
			oscrc: "[https://api.opensuse.org]\nuser=me\n",
			want:  "",
		},
		{
			name: "undecodable obfuscated pass",
			oscrc: "[https://api.opensuse.org]\nuser=me\npass=plain-secret\n" +
				"credentials_mgr_class=osc.credentials.ObfuscatedConfigFileCredentialsManager\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := configPassword(writeConfig(t, tt.oscrc), section)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
