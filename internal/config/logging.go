package config

import (
	"github.com/spf13/viper"

	"github.com/anchore/go-logger"
)

// logging contains all logging-related configuration options available to the user via the application config.
type logging struct {
	Structured   bool         `yaml:"structured" json:"structured" mapstructure:"structured"`   // show all log entries as JSON formatted strings
	LevelOpt     logger.Level `yaml:"-" json:"-"`                                               // the native log level object used by the logger
	Level        string       `yaml:"level" json:"level" mapstructure:"level"`                  // the log level string hint
	FileLocation string       `yaml:"file,omitempty" json:"file,omitempty" mapstructure:"file"` // the file path to write logs to
}

func (cfg logging) loadDefaultValues(v *viper.Viper) {
	v.SetDefault("log.structured", false)
	v.SetDefault("log.level", string(logger.WarnLevel))
}
