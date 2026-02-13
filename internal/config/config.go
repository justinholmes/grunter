package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var timeFormatRe = regexp.MustCompile(`^\d{2}:\d{2}$`)

// RFCSource identifies the system that manages RFCs/change requests.
type RFCSource string

const (
	RFCSourceGitLab     RFCSource = "gitlab"
	RFCSourceServiceNow RFCSource = "servicenow"
	RFCSourceCustom     RFCSource = "custom"
)

// DeploymentWindowSource identifies the type of deployment window check.
type DeploymentWindowSource string

const (
	DeploymentWindowSchedule DeploymentWindowSource = "schedule"
	DeploymentWindowCustom   DeploymentWindowSource = "custom"
)

// RFCConfig defines how RFC/change request verification is performed for an environment.
type RFCConfig struct {
	Source             RFCSource `yaml:"source"`
	ApprovedLabel      string    `yaml:"approved_label,omitempty"`
	IssueApprovedLabel string    `yaml:"issue_approved_label,omitempty"`
	EmergencyLabel     string    `yaml:"emergency_label,omitempty"`
	InstanceURL        string    `yaml:"instance_url,omitempty"`
	ChangeNumberVar    string    `yaml:"change_number_var,omitempty"`

	// Custom source fields
	URL             string `yaml:"url,omitempty"`
	Method          string `yaml:"method,omitempty"`
	AuthType        string `yaml:"auth_type,omitempty"`
	AuthTokenVar    string `yaml:"auth_token_var,omitempty"`
	AuthUsernameVar string `yaml:"auth_username_var,omitempty"`
	AuthHeaderName  string `yaml:"auth_header_name,omitempty"`
	ResponseType    string `yaml:"response_type,omitempty"`
	ResponsePath    string `yaml:"response_path,omitempty"`
	ApprovedField   string `yaml:"approved_field,omitempty"`
	ApprovedValue   string `yaml:"approved_value,omitempty"`
	EmergencyField  string `yaml:"emergency_field,omitempty"`
	EmergencyValue  string `yaml:"emergency_value,omitempty"`
	IdentifierField string `yaml:"identifier_field,omitempty"`
}

// DeploymentWindow restricts deployments to specific days and times.
type DeploymentWindow struct {
	Source   DeploymentWindowSource `yaml:"source,omitempty"`
	Days    []string               `yaml:"days"`
	Start   string                 `yaml:"start"`
	End     string                 `yaml:"end"`
	Timezone string                `yaml:"timezone"`

	// Custom source fields
	URL             string `yaml:"url,omitempty"`
	Method          string `yaml:"method,omitempty"`
	AuthType        string `yaml:"auth_type,omitempty"`
	AuthTokenVar    string `yaml:"auth_token_var,omitempty"`
	AuthUsernameVar string `yaml:"auth_username_var,omitempty"`
	AuthHeaderName  string `yaml:"auth_header_name,omitempty"`
	ResponsePath    string `yaml:"response_path,omitempty"`
	AllowedField    string `yaml:"allowed_field,omitempty"`
	MessageField    string `yaml:"message_field,omitempty"`
	NextWindowField string `yaml:"next_window_field,omitempty"`
}

// Environment defines a deployment environment with its filesystem path and regions.
type Environment struct {
	Name             string            `yaml:"name"`
	Path             string            `yaml:"path"`
	Regions          []string          `yaml:"regions"`
	ApprovalGroup    string            `yaml:"approval_group,omitempty"`
	RFC              *RFCConfig        `yaml:"rfc,omitempty"`
	DeploymentWindow *DeploymentWindow `yaml:"deployment_window,omitempty"`
}

// RequiresRFCCheck returns true if the environment has RFC configuration.
func (e Environment) RequiresRFCCheck() bool {
	return e.RFC != nil
}

// HasDeploymentWindow returns true if the environment has a deployment window configured.
func (e Environment) HasDeploymentWindow() bool {
	return e.DeploymentWindow != nil
}

// IsMultiRegion returns true if the environment has more than one region.
func (e Environment) IsMultiRegion() bool {
	return len(e.Regions) > 1
}

// Config holds the parsed .grunter/config.yml values.
type Config struct {
	DeployBranch string        `yaml:"deploy_branch"`
	TFBinary     string        `yaml:"tf_binary"`
	TGBinary     string        `yaml:"tg_binary"`
	Ignore       []string      `yaml:"ignore"`
	Environments []Environment `yaml:"environments"`
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

// Validate checks that the configuration is internally consistent.
// It is safe to call when no environments are defined (backward-compatible).
func (c *Config) Validate() error {
	seen := make(map[string]bool)
	for _, env := range c.Environments {
		if env.Name == "" {
			return fmt.Errorf("environment name is required")
		}
		if env.Path == "" {
			return fmt.Errorf("environment %q: path is required", env.Name)
		}
		if seen[env.Name] {
			return fmt.Errorf("duplicate environment name %q", env.Name)
		}
		seen[env.Name] = true

		if err := validateRFCConfig(env.Name, env.RFC); err != nil {
			return err
		}
		if err := validateDeploymentWindow(env.Name, env.DeploymentWindow); err != nil {
			return err
		}
	}
	return nil
}

var validDays = map[string]bool{
	"monday": true, "tuesday": true, "wednesday": true,
	"thursday": true, "friday": true, "saturday": true, "sunday": true,
}

func validateRFCConfig(envName string, rfc *RFCConfig) error {
	if rfc == nil {
		return nil
	}
	switch rfc.Source {
	case RFCSourceGitLab:
		if rfc.ApprovedLabel == "" {
			return fmt.Errorf("environment %q: rfc.approved_label is required for gitlab source", envName)
		}
	case RFCSourceServiceNow:
		if rfc.InstanceURL == "" {
			return fmt.Errorf("environment %q: rfc.instance_url is required for servicenow source", envName)
		}
		if rfc.ChangeNumberVar == "" {
			return fmt.Errorf("environment %q: rfc.change_number_var is required for servicenow source", envName)
		}
	case RFCSourceCustom:
		if rfc.URL == "" {
			return fmt.Errorf("environment %q: rfc.url is required for custom source", envName)
		}
		if rfc.ApprovedField == "" {
			return fmt.Errorf("environment %q: rfc.approved_field is required for custom source", envName)
		}
		if rfc.ApprovedValue == "" {
			return fmt.Errorf("environment %q: rfc.approved_value is required for custom source", envName)
		}
		if err := validateAuthFields(envName, "rfc", rfc.AuthType, rfc.AuthTokenVar, rfc.AuthUsernameVar, rfc.AuthHeaderName); err != nil {
			return err
		}
	default:
		return fmt.Errorf("environment %q: rfc.source must be %q, %q, or %q", envName, RFCSourceGitLab, RFCSourceServiceNow, RFCSourceCustom)
	}
	return nil
}

func validateDeploymentWindow(envName string, w *DeploymentWindow) error {
	if w == nil {
		return nil
	}

	source := w.Source
	if source == "" {
		source = DeploymentWindowSchedule
	}

	switch source {
	case DeploymentWindowSchedule:
		if len(w.Days) == 0 {
			return fmt.Errorf("environment %q: deployment_window.days must not be empty", envName)
		}
		for _, d := range w.Days {
			if !validDays[strings.ToLower(d)] {
				return fmt.Errorf("environment %q: invalid day %q in deployment_window", envName, d)
			}
		}
		if !timeFormatRe.MatchString(w.Start) {
			return fmt.Errorf("environment %q: deployment_window.start must be HH:MM format", envName)
		}
		if !timeFormatRe.MatchString(w.End) {
			return fmt.Errorf("environment %q: deployment_window.end must be HH:MM format", envName)
		}
		if w.Timezone == "" {
			return fmt.Errorf("environment %q: deployment_window.timezone is required", envName)
		}
		if _, err := time.LoadLocation(w.Timezone); err != nil {
			return fmt.Errorf("environment %q: invalid timezone %q: %w", envName, w.Timezone, err)
		}
	case DeploymentWindowCustom:
		if w.URL == "" {
			return fmt.Errorf("environment %q: deployment_window.url is required for custom source", envName)
		}
		if w.AllowedField == "" {
			return fmt.Errorf("environment %q: deployment_window.allowed_field is required for custom source", envName)
		}
		if err := validateAuthFields(envName, "deployment_window", w.AuthType, w.AuthTokenVar, w.AuthUsernameVar, w.AuthHeaderName); err != nil {
			return err
		}
	default:
		return fmt.Errorf("environment %q: deployment_window.source must be %q or %q", envName, DeploymentWindowSchedule, DeploymentWindowCustom)
	}

	return nil
}

func validateAuthFields(envName, section, authType, tokenVar, usernameVar, headerName string) error {
	if authType == "" {
		return nil
	}
	switch authType {
	case "bearer":
		if tokenVar == "" {
			return fmt.Errorf("environment %q: %s.auth_token_var is required for bearer auth", envName, section)
		}
	case "basic":
		if tokenVar == "" {
			return fmt.Errorf("environment %q: %s.auth_token_var is required for basic auth (password)", envName, section)
		}
		if usernameVar == "" {
			return fmt.Errorf("environment %q: %s.auth_username_var is required for basic auth", envName, section)
		}
	case "header":
		if tokenVar == "" {
			return fmt.Errorf("environment %q: %s.auth_token_var is required for header auth", envName, section)
		}
		if headerName == "" {
			return fmt.Errorf("environment %q: %s.auth_header_name is required for header auth", envName, section)
		}
	default:
		return fmt.Errorf("environment %q: %s.auth_type must be \"bearer\", \"basic\", or \"header\"", envName, section)
	}
	return nil
}

// GetEnvironment returns the environment with the given name, or nil if not found.
func (c *Config) GetEnvironment(name string) *Environment {
	for i := range c.Environments {
		if c.Environments[i].Name == name {
			return &c.Environments[i]
		}
	}
	return nil
}

// EnvironmentIndex returns the index of the named environment in the promotion
// sequence, or -1 if not found.
func (c *Config) EnvironmentIndex(name string) int {
	for i, env := range c.Environments {
		if env.Name == name {
			return i
		}
	}
	return -1
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

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}
