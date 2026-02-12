package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the parsed .grunter/config.yml values.
type Config struct {
	DeployBranch string   `yaml:"deploy_branch"`
	TFBinary     string   `yaml:"tf_binary"`
	TGBinary     string   `yaml:"tg_binary"`
	Ignore       []string `yaml:"ignore"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		DeployBranch: "main",
		TFBinary:     "opentofu",
		TGBinary:     "terragrunt",
		Ignore: []string{
			"**/.terraform.lock.hcl",
			"**/README.md",
		},
	}
}

// Load reads and parses the config file at the given path.
// If the file does not exist, it returns the default config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.DeployBranch == "" {
		cfg.DeployBranch = "main"
	}
	if cfg.TFBinary == "" {
		cfg.TFBinary = "opentofu"
	}
	if cfg.TGBinary == "" {
		cfg.TGBinary = "terragrunt"
	}

	return cfg, nil
}
