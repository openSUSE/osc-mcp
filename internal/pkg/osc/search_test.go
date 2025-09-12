package osc

import (
	"reflect"
	"testing"
)

func TestParseRPMFileName(t *testing.T) {
	testCases := []struct {
		name     string
		filename string
		expected rpm_pack
	}{
		{
			name:     "simple package",
			filename: "pkg-name-1.2.3-1.x86_64.rpm",
			expected: rpm_pack{Name: "pkg-name", Version: "1.2.3-1", Arch: "x86_64"},
		},
		{
			name:     "glibc-devel",
			filename: "glibc-devel-2.31-150400.1.1.x86_64.rpm",
			expected: rpm_pack{Name: "glibc-devel", Version: "2.31-150400.1.1", Arch: "x86_64"},
		},
		{
			name:     "package with number in name",
			filename: "libvpx7-1.11.0-150400.1.1.x86_64.rpm",
			expected: rpm_pack{Name: "libvpx7", Version: "1.11.0-150400.1.1", Arch: "x86_64"},
		},
		{
			name:     "noarch package",
			filename: "yast2-trans-stats-2.20.0-1.1.noarch.rpm",
			expected: rpm_pack{Name: "yast2-trans-stats", Version: "2.20.0-1.1", Arch: "noarch"},
		},
		{
			name:     "kernel package",
			filename: "kernel-default-5.14.21-150400.22.1.x86_64.rpm",
			expected: rpm_pack{Name: "kernel-default", Version: "5.14.21-150400.22.1", Arch: "x86_64"},
		},
		{
			name:     "ambiguous name",
			filename: "foo-1-2-3.x86_64.rpm",
			expected: rpm_pack{Name: "foo-1", Version: "2-3", Arch: "x86_64"},
		},
		{
			name:     "long name",
			filename: "foo-bar-1.2.3-lp152.1.1.x86_64.rpm",
			expected: rpm_pack{Name: "foo-bar", Version: "1.2.3-lp152.1.1", Arch: "x86_64"},
		},
		{
			name:     "no version",
			filename: "no-version.x86_64.rpm",
			expected: rpm_pack{Name: "no-version", Arch: "x86_64"},
		},
		{
			name:     "not an rpm",
			filename: "invalid-rpm",
			expected: rpm_pack{},
		},
		{
			name:     "not an rpm with suffix",
			filename: "another.invalid.rpm.txt",
			expected: rpm_pack{},
		},
		{
			name:     "no arch",
			filename: "no-arch.rpm",
			expected: rpm_pack{},
		},
		{
			name:     "no release",
			filename: "no-release-1.2.3.x86_64.rpm",
			expected: rpm_pack{Name: "no-release", Version: "1.2.3", Arch: "x86_64"},
		},
		{
			name:     "version but no release",
			filename: "package-1.0.x86_64.rpm",
			expected: rpm_pack{Name: "package", Version: "1.0", Arch: "x86_64"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := parseRPMFileName(tc.filename)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("For filename '%s', expected %+v but got %+v", tc.filename, tc.expected, actual)
			}
		})
	}
}
