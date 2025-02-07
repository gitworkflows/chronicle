package config

import (
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"

	"github.com/adrg/xdg"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"

	"github.com/anchore/chronicle/internal"
	"github.com/anchore/go-logger"
)

var ErrApplicationConfigNotFound = fmt.Errorf("application config not found")

type defaultValueLoader interface {
	loadDefaultValues(*viper.Viper)
}

type parser interface {
	parseConfigValues() error
}

type Application struct {
	ConfigPath           string           `yaml:",omitempty" json:"configPath"`                                                               // the location where the application config was read from (either from -c or discovered while loading)
	Output               string           `yaml:"output" json:"output" mapstructure:"output"`                                                 // -o, the Presenter hint string to use for report formatting
	Quiet                bool             `yaml:"quiet" json:"quiet" mapstructure:"quiet"`                                                    // -q, indicates to not show any status output to stderr (ETUI or logging UI)
	Log                  logging          `yaml:"log" json:"log" mapstructure:"log"`                                                          // all logging-related options
	CliOptions           CliOnlyOptions   `yaml:"-" json:"-"`                                                                                 // all options only available through the CLI (not via env vars or config)
	SpeculateNextVersion bool             `yaml:"speculate-next-version" json:"speculate-next-version" mapstructure:"speculate-next-version"` // -n, guess the next version based on issues and PRs
	VersionFile          string           `yaml:"version-file" json:"version-file" mapstructure:"version-file"`                               // --version-file, the path to a file containing the version to use for the changelog
	SinceTag             string           `yaml:"since-tag" json:"since-tag" mapstructure:"since-tag"`                                        // -s, the tag to start the changelog from
	UntilTag             string           `yaml:"until-tag" json:"until-tag" mapstructure:"until-tag"`                                        // -u, the tag to end the changelog at
	EnforceV0            bool             `yaml:"enforce-v0" json:"enforce-v0" mapstructure:"enforce-v0"`
	Title                string           `yaml:"title" json:"title" mapstructure:"title"`
	Github               githubSummarizer `yaml:"github" json:"github" mapstructure:"github"`
}

func newApplicationConfig(v *viper.Viper, cliOpts CliOnlyOptions) *Application {
	config := &Application{
		CliOptions: cliOpts,
	}
	config.loadDefaultValues(v)
	return config
}

// LoadApplicationConfig populates the given viper object with application configuration discovered on disk
func LoadApplicationConfig(v *viper.Viper, cliOpts CliOnlyOptions) (*Application, error) {
	// the user may not have a config, and this is OK, we can use the default config + default cobra cli values instead
	config := newApplicationConfig(v, cliOpts)

	if err := readConfig(v, cliOpts.ConfigPath); err != nil && !errors.Is(err, ErrApplicationConfigNotFound) {
		return nil, err
	}

	if err := v.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("unable to parse config: %w", err)
	}
	config.ConfigPath = v.ConfigFileUsed()

	if err := config.parseConfigValues(); err != nil {
		return nil, fmt.Errorf("invalid application config: %w", err)
	}

	return config, nil
}

// init loads the default configuration values into the viper instance (before the config values are read and parsed).
func (cfg Application) loadDefaultValues(v *viper.Viper) {
	// set the default values for primitive fields in this struct
	// TODO...

	// for each field in the configuration struct, see if the field implements the defaultValueLoader interface and invoke it if it does
	value := reflect.ValueOf(cfg)
	for i := 0; i < value.NumField(); i++ {
		// note: the defaultValueLoader method receiver is NOT a pointer receiver.
		if loadable, ok := value.Field(i).Interface().(defaultValueLoader); ok {
			// the field implements defaultValueLoader, call it
			loadable.loadDefaultValues(v)
		}
	}
}

// build inflates simple config values into native objects (or other complex objects) after the config is fully read in.
func (cfg *Application) parseConfigValues() error {
	if cfg.SpeculateNextVersion && cfg.UntilTag != "" {
		return errors.New("cannot specify both --speculate-next-version and --until-tag")
	}

	if cfg.Quiet {
		cfg.Log.LevelOpt = logger.DisabledLevel
	} else {
		if cfg.CliOptions.Verbosity > 0 {
			// set the log level implicitly
			switch v := cfg.CliOptions.Verbosity; {
			case v == 1:
				cfg.Log.LevelOpt = logger.InfoLevel
			case v == 2:
				cfg.Log.LevelOpt = logger.DebugLevel
			case v >= 3:
				cfg.Log.LevelOpt = logger.TraceLevel
			default:
				cfg.Log.LevelOpt = logger.WarnLevel
			}
			cfg.Log.Level = string(cfg.Log.LevelOpt)
		} else {
			lvl, err := logger.LevelFromString(strings.ToLower(cfg.Log.Level))
			if err != nil {
				return fmt.Errorf("bad log level configured (%q): %w", cfg.Log.Level, err)
			}
			// set the log level explicitly
			cfg.Log.LevelOpt = lvl
		}
	}

	// for each field in the configuration struct, see if the field implements the parser interface
	// note: the app config is a pointer, so we need to grab the elements explicitly (to traverse the address)
	value := reflect.ValueOf(cfg).Elem()
	for i := 0; i < value.NumField(); i++ {
		// note: since the interface method of parser is a pointer receiver we need to get the value of the field as a pointer.
		if parsable, ok := value.Field(i).Addr().Interface().(parser); ok {
			// the field implements parser, call it
			if err := parsable.parseConfigValues(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (cfg Application) String() string {
	// yaml is pretty human friendly (at least when compared to json)
	appCfgStr, err := yaml.Marshal(&cfg)

	if err != nil {
		return err.Error()
	}

	return string(appCfgStr)
}

// readConfig attempts to read the given config path from disk or discover an alternate store location
// nolint:funlen
func readConfig(v *viper.Viper, configPath string) error {
	var err error
	v.AutomaticEnv()
	v.SetEnvPrefix(internal.ApplicationName)
	// allow for nested options to be specified via environment variables
	// e.g. pod.context = APPNAME_POD_CONTEXT
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))

	// use explicitly the given user config
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("unable to read application config=%q : %w", configPath, err)
		}
		// don't fall through to other options if the config path was explicitly provided
		return nil
	}

	// start searching for valid configs in order...

	// 1. look for .<appname>.yaml (in the current directory)
	v.AddConfigPath(".")
	v.SetConfigName("." + internal.ApplicationName)
	if err = v.ReadInConfig(); err == nil {
		return nil
	} else if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
		return fmt.Errorf("unable to parse config=%q: %w", v.ConfigFileUsed(), err)
	}

	// 2. look for .<appname>/config.yaml (in the current directory)
	v.AddConfigPath("." + internal.ApplicationName)
	v.SetConfigName("config")
	if err = v.ReadInConfig(); err == nil {
		return nil
	} else if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
		return fmt.Errorf("unable to parse config=%q: %w", v.ConfigFileUsed(), err)
	}

	// 3. look for ~/.<appname>.yaml
	home, err := homedir.Dir()
	if err == nil {
		v.AddConfigPath(home)
		v.SetConfigName("." + internal.ApplicationName)
		if err = v.ReadInConfig(); err == nil {
			return nil
		} else if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
			return fmt.Errorf("unable to parse config=%q: %w", v.ConfigFileUsed(), err)
		}
	}

	// 4. look for <appname>/config.yaml in xdg locations (starting with xdg home config dir, then moving upwards)
	v.AddConfigPath(path.Join(xdg.ConfigHome, internal.ApplicationName))
	for _, dir := range xdg.ConfigDirs {
		v.AddConfigPath(path.Join(dir, internal.ApplicationName))
	}
	v.SetConfigName("config")
	if err = v.ReadInConfig(); err == nil {
		return nil
	} else if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
		return fmt.Errorf("unable to parse config=%q: %w", v.ConfigFileUsed(), err)
	}

	return ErrApplicationConfigNotFound
}
