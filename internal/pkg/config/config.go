package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds the parsed configuration data.
type Config struct {
	data map[string]map[string]string
}

// NewConfig creates a new Config object.
func NewConfig(path string) (newConf *Config, err error) {
	newConf = &Config{
		data: make(map[string]map[string]string),
	}
	if path == "" {
		return newConf, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return newConf, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	currentSection := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = line[1 : len(line)-1]
			if _, ok := newConf.data[currentSection]; !ok {
				newConf.data[currentSection] = make(map[string]string)
			}
			continue
		}

		if currentSection != "" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				newConf.data[currentSection][key] = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return newConf, fmt.Errorf("failed to read config file: %w", err)
	}

	return newConf, nil
}

// GetString returns the string value for a given section and key.
func (c *Config) GetString(section, key string) string {
	if sec, ok := c.data[section]; ok {
		return sec[key]
	}
	return ""
}

// GetBool returns the boolean value for a given section and key.
func (c *Config) GetBool(section, key string) bool {
	valStr := c.GetString(section, key)
	if valStr == "" {
		return false
	}
	b, err := strconv.ParseBool(valStr)
	if err != nil {
		// Also check for 1/0 for bools
		if valStr == "1" {
			return true
		}
		if valStr == "0" {
			return false
		}
		return false
	}
	return b
}
