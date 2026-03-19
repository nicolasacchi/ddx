package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Project struct {
	APIKey string `toml:"api_key"`
	AppKey string `toml:"app_key"`
	Site   string `toml:"site,omitempty"`
}

type Config struct {
	DefaultProject string              `toml:"default_project,omitempty"`
	Projects       map[string]*Project `toml:"projects,omitempty"`
}

func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ddx", "config.toml"), nil
}

func loadConfigFile() (*Config, error) {
	path, err := configFilePath()
	if err != nil {
		return nil, err
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfigFile(cfg *Config) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

func resolveProject(cfg *Config, projectFlag string) *Project {
	if cfg == nil {
		return nil
	}
	if projectFlag != "" && cfg.Projects != nil {
		if p, ok := cfg.Projects[projectFlag]; ok {
			return p
		}
		return nil
	}
	if cfg.DefaultProject != "" && cfg.Projects != nil {
		if p, ok := cfg.Projects[cfg.DefaultProject]; ok {
			return p
		}
	}
	return nil
}

// Credentials holds resolved API key, App key, and site.
type Credentials struct {
	APIKey string
	AppKey string
	Site   string
}

// LoadCredentials resolves credentials from flag > env > config file.
func LoadCredentials(apiKeyFlag, appKeyFlag, siteFlag, projectFlag string) (*Credentials, error) {
	creds := &Credentials{}

	// API Key
	switch {
	case apiKeyFlag != "":
		creds.APIKey = apiKeyFlag
	case os.Getenv("DD_API_KEY") != "":
		creds.APIKey = os.Getenv("DD_API_KEY")
	default:
		cfg, err := loadConfigFile()
		if err == nil {
			if p := resolveProject(cfg, projectFlag); p != nil {
				creds.APIKey = p.APIKey
				creds.AppKey = p.AppKey
				creds.Site = p.Site
			}
		}
	}

	// App Key (override if flag/env set)
	if appKeyFlag != "" {
		creds.AppKey = appKeyFlag
	} else if v := os.Getenv("DD_APP_KEY"); v != "" {
		creds.AppKey = v
	}

	// Site (override if flag/env set)
	if siteFlag != "" {
		creds.Site = siteFlag
	} else if v := os.Getenv("DD_SITE"); v != "" {
		creds.Site = v
	}
	if creds.Site == "" {
		creds.Site = "datadoghq.eu"
	}

	if creds.APIKey == "" {
		return nil, fmt.Errorf("API key required: use --api-key flag, DD_API_KEY env var, or run 'ddx config add'")
	}
	if creds.AppKey == "" {
		return nil, fmt.Errorf("App key required: use --app-key flag, DD_APP_KEY env var, or run 'ddx config add'")
	}

	return creds, nil
}

func AddProject(name, apiKey, appKey, site string) error {
	cfg, err := loadConfigFile()
	if err != nil {
		cfg = &Config{}
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]*Project)
	}
	cfg.Projects[name] = &Project{
		APIKey: apiKey,
		AppKey: appKey,
		Site:   site,
	}
	if cfg.DefaultProject == "" {
		cfg.DefaultProject = name
	}
	return saveConfigFile(cfg)
}

func RemoveProject(name string) error {
	cfg, err := loadConfigFile()
	if err != nil {
		return fmt.Errorf("no config file found")
	}
	if cfg.Projects == nil {
		return fmt.Errorf("project %q not found", name)
	}
	if _, ok := cfg.Projects[name]; !ok {
		return fmt.Errorf("project %q not found", name)
	}
	delete(cfg.Projects, name)
	if cfg.DefaultProject == name {
		cfg.DefaultProject = ""
		for k := range cfg.Projects {
			cfg.DefaultProject = k
			break
		}
	}
	if len(cfg.Projects) == 0 {
		cfg.Projects = nil
	}
	return saveConfigFile(cfg)
}

func SetDefaultProject(name string) error {
	cfg, err := loadConfigFile()
	if err != nil {
		return fmt.Errorf("no config file found")
	}
	if cfg.Projects == nil {
		return fmt.Errorf("project %q not found", name)
	}
	if _, ok := cfg.Projects[name]; !ok {
		return fmt.Errorf("project %q not found", name)
	}
	cfg.DefaultProject = name
	return saveConfigFile(cfg)
}

func ListProjects() (*Config, error) {
	return loadConfigFile()
}

func MaskKey(key string) string {
	if len(key) <= 10 {
		return "***"
	}
	return key[:8] + "***" + key[len(key)-4:]
}
