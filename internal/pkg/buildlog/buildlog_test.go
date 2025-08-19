package buildlog

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLog(t *testing.T) {
	testCases := []struct {
		name            string
		logFile         string
		expectedName    string
		expectedProject string
		expectedDistro  string
		expectedArch    string
		expectedPhases  map[BuildPhase]struct {
			lineCount int
			duration  int
		}
	}{
		{
			name:            "gflags_aarch64",
			logFile:         "testdata/gflags_aarch64.log",
			expectedName:    "gflags",
			expectedProject: "home:mslacken:ml",
			expectedDistro:  "16.0",
			expectedArch:    "aarch64",
			expectedPhases: map[BuildPhase]struct {
				lineCount int
				duration  int
			}{
				Header:              {lineCount: 14, duration: 2},
				Preinstall:          {lineCount: 45, duration: 1},
				CopyingPackages:     {lineCount: 9, duration: 2},
				VMBoot:              {lineCount: 22, duration: 4},
				PackageCumulation:   {lineCount: 143, duration: 0},
				PackageInstallation: {lineCount: 166, duration: 7},
				Build:               {lineCount: 591, duration: 22},
				PostBuildChecks:     {lineCount: 38, duration: 1},
				RPMLintReport:       {lineCount: 39, duration: 1},
				PackageComparison:   {lineCount: 502, duration: 1},
				Summary:             {lineCount: 16, duration: 0},
				Retries:             {lineCount: 9, duration: 0},
			},
		},
		{
			name:            "ww4_local",
			logFile:         "testdata/local-ww4.log",
			expectedName:    "warewulf4",
			expectedProject: "local",
			expectedDistro:  "15.6",
			expectedArch:    "x86_64",
			expectedPhases: map[BuildPhase]struct {
				lineCount int
				duration  int
			}{
				Header:          {lineCount: 894, duration: 5},
				Build:           {lineCount: 2198, duration: 36},
				PostBuildChecks: {lineCount: 47, duration: 2},
				RPMLintReport:   {lineCount: 51, duration: 3},
				Summary:         {lineCount: 2, duration: 0},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logContent, err := os.ReadFile(tc.logFile)
			assert.NoError(t, err)

			log, err := ParseLog(string(logContent))
			assert.NoError(t, err)

			assert.Equal(t, tc.expectedName, log.Name)
			assert.Equal(t, tc.expectedProject, log.Project)
			assert.Equal(t, tc.expectedDistro, log.Distro)
			assert.Equal(t, tc.expectedArch, log.Arch)
			assert.NotNil(t, log.rawlog)

			assert.Equal(t, len(tc.expectedPhases), len(log.Phases))

			for phase, expected := range tc.expectedPhases {
				actual, ok := log.Phases[phase]
				assert.True(t, ok, "Expected phase %s not found", phase.String())
				assert.Equal(t, expected.lineCount, len(actual.Lines), "Line count mismatch for phase %s", phase.String())
				assert.Equal(t, expected.duration, actual.Duration, "Duration mismatch for phase %s", phase.String())
			}
		})
	}
}
