package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	adminColorAuto   = "auto"
	adminColorAlways = "always"
	adminColorNever  = "never"

	adminThemeDefault      = "default"
	adminThemeMono         = "mono"
	adminThemeHighContrast = "high-contrast"

	adminDensityComfortable = "comfortable"
	adminDensityCompact     = "compact"
)

type adminUIConfig struct {
	Color       string `json:"color"`
	Theme       string `json:"theme"`
	Density     string `json:"density"`
	ShowHash    bool   `json:"showHash"`
	WrapCommand bool   `json:"wrapCommand"`
}

type adminWatchOptions struct {
	Interval time.Duration
	UI       adminUIConfig
}

func defaultAdminUIConfig() adminUIConfig {
	return adminUIConfig{
		Color:       adminColorAuto,
		Theme:       adminThemeDefault,
		Density:     adminDensityComfortable,
		ShowHash:    true,
		WrapCommand: true,
	}
}

func adminWatchUsage() string {
	return "Usage: rootbroker-admin watch [--interval SECONDS] [--color auto|always|never]\n" +
		"                              [--theme default|mono|high-contrast]\n" +
		"                              [--density comfortable|compact]\n" +
		"                              [--show-hash=BOOL] [--wrap-command=BOOL]\n" +
		"                              [--config PATH]"
}

func parseAdminWatchOptions(args []string) (adminWatchOptions, bool, error) {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return adminWatchOptions{}, true, nil
		}
	}
	configPath, explicitConfig, err := adminUIConfigPath(args)
	if err != nil {
		return adminWatchOptions{}, false, err
	}
	ui, err := loadAdminUIConfig(configPath, explicitConfig)
	if err != nil {
		return adminWatchOptions{}, false, err
	}
	if err := applyAdminUIEnvironment(&ui); err != nil {
		return adminWatchOptions{}, false, err
	}

	flags := flag.NewFlagSet("rootbroker-admin watch", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	interval := flags.Float64("interval", 1, "poll interval in seconds")
	flags.StringVar(&ui.Color, "color", ui.Color, "color mode")
	flags.StringVar(&ui.Theme, "theme", ui.Theme, "display theme")
	flags.StringVar(&ui.Density, "density", ui.Density, "display density")
	flags.BoolVar(&ui.ShowHash, "show-hash", ui.ShowHash, "show the request hash")
	flags.BoolVar(&ui.WrapCommand, "wrap-command", ui.WrapCommand, "wrap command arguments")
	flags.String("config", configPath, "UI configuration file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return adminWatchOptions{}, true, nil
		}
		return adminWatchOptions{}, false, fmt.Errorf("invalid watch arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return adminWatchOptions{}, false, fmt.Errorf("invalid watch arguments")
	}
	maximumInterval := float64(time.Duration(1<<63-1)) / float64(time.Second)
	if math.IsNaN(*interval) || math.IsInf(*interval, 0) || *interval < 0.1 || *interval > maximumInterval {
		return adminWatchOptions{}, false, fmt.Errorf("--interval must be a finite value of at least 0.1")
	}
	if err := validateAdminUIConfig(ui); err != nil {
		return adminWatchOptions{}, false, err
	}
	return adminWatchOptions{Interval: time.Duration(*interval * float64(time.Second)), UI: ui}, false, nil
}

func adminUIConfigPath(args []string) (string, bool, error) {
	var path string
	explicit := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--config":
			if index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "--") {
				return "", false, fmt.Errorf("--config requires PATH")
			}
			path, explicit = args[index+1], true
			index++
		case strings.HasPrefix(arg, "--config="):
			path, explicit = strings.TrimPrefix(arg, "--config="), true
			if path == "" {
				return "", false, fmt.Errorf("--config requires PATH")
			}
		}
	}
	if explicit {
		return filepath.Clean(path), true, nil
	}
	if path = os.Getenv("ROOTBROKER_ADMIN_CONFIG"); path != "" {
		return filepath.Clean(path), true, nil
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "rootbroker", "admin.json"), false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, nil
	}
	return filepath.Join(home, ".config", "rootbroker", "admin.json"), false, nil
}

func loadAdminUIConfig(path string, explicit bool) (adminUIConfig, error) {
	config := defaultAdminUIConfig()
	if path == "" {
		return config, nil
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return config, nil
		}
		return adminUIConfig{}, fmt.Errorf("open admin UI configuration: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return adminUIConfig{}, fmt.Errorf("decode admin UI configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return adminUIConfig{}, fmt.Errorf("decode admin UI configuration: trailing JSON value")
		}
		return adminUIConfig{}, fmt.Errorf("decode admin UI configuration: %w", err)
	}
	if err := validateAdminUIConfig(config); err != nil {
		return adminUIConfig{}, fmt.Errorf("admin UI configuration: %w", err)
	}
	return config, nil
}

func applyAdminUIEnvironment(config *adminUIConfig) error {
	if value := os.Getenv("NO_COLOR"); value != "" {
		config.Color = adminColorNever
	}
	if value := os.Getenv("ROOTBROKER_COLOR"); value != "" {
		config.Color = value
	}
	if value := os.Getenv("ROOTBROKER_THEME"); value != "" {
		config.Theme = value
	}
	if value := os.Getenv("ROOTBROKER_DENSITY"); value != "" {
		config.Density = value
	}
	if value := os.Getenv("ROOTBROKER_SHOW_HASH"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("ROOTBROKER_SHOW_HASH must be true or false")
		}
		config.ShowHash = parsed
	}
	if value := os.Getenv("ROOTBROKER_WRAP_COMMAND"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("ROOTBROKER_WRAP_COMMAND must be true or false")
		}
		config.WrapCommand = parsed
	}
	return validateAdminUIConfig(*config)
}

func validateAdminUIConfig(config adminUIConfig) error {
	if config.Color != adminColorAuto && config.Color != adminColorAlways && config.Color != adminColorNever {
		return fmt.Errorf("color must be auto, always, or never")
	}
	if config.Theme != adminThemeDefault && config.Theme != adminThemeMono && config.Theme != adminThemeHighContrast {
		return fmt.Errorf("theme must be default, mono, or high-contrast")
	}
	if config.Density != adminDensityComfortable && config.Density != adminDensityCompact {
		return fmt.Errorf("density must be comfortable or compact")
	}
	return nil
}

func shouldUseANSI(mode string, isTTY bool, terminal string) bool {
	switch mode {
	case adminColorAlways:
		return true
	case adminColorNever:
		return false
	default:
		return isTTY && terminal != "dumb"
	}
}

func adminTerminal(output *os.File) (bool, int) {
	isTTY := term.IsTerminal(int(output.Fd()))
	width := 100
	if isTTY {
		if columns, _, err := term.GetSize(int(output.Fd())); err == nil && columns > 0 {
			width = columns
		}
	} else if columns, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && columns > 0 {
		width = columns
	}
	if width < 48 {
		width = 48
	}
	return isTTY, width
}
