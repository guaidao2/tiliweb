package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML-friendly time.Duration ("30s", "12h", "720h").
type Duration time.Duration

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a string")
	}
	s := strings.TrimSpace(value.Value)
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) Set(s string) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}
