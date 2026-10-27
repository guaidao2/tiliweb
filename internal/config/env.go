package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// ApplyEnv overlays TILIWEB_* variables onto the config. Env names are derived
// mechanically from YAML paths:
//
//	server.listen      -> TILIWEB_SERVER_LISTEN
//	auth.register_mode -> TILIWEB_AUTH_REGISTER_MODE
//	mcp.http.tls.enabled -> TILIWEB_MCP_HTTP_TLS_ENABLED
func (c *Config) ApplyEnv() error {
	return applyEnvValue(reflect.ValueOf(c).Elem(), "TILIWEB")
}

func applyEnvValue(v reflect.Value, prefix string) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		name := prefix + "_" + strings.ToUpper(strings.ReplaceAll(tag, "-", "_"))
		fv := v.Field(i)

		if f.Type == reflect.TypeOf(Duration(0)) {
			raw, ok := os.LookupEnv(name)
			if !ok {
				continue
			}
			if err := fv.Addr().Interface().(*Duration).Set(raw); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			continue
		}

		if fv.Kind() == reflect.Struct {
			if err := applyEnvValue(fv, name); err != nil {
				return err
			}
			continue
		}

		raw, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if err := setField(fv, raw); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func setField(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("invalid bool: %w", err)
		}
		fv.SetBool(b)
	case reflect.Int:
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("invalid int: %w", err)
		}
		fv.SetInt(int64(n))
	case reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int64: %w", err)
		}
		fv.SetInt(n)
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice type %s", fv.Type())
		}
		parts := []string{}
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				parts = append(parts, p)
			}
		}
		fv.Set(reflect.ValueOf(parts))
	default:
		return fmt.Errorf("unsupported type %s", fv.Type())
	}
	return nil
}
