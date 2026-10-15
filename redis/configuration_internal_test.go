package redis

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

const (
	souinAddress = "redis-souin:6379"
	initAddress  = "InitAddress"
)

func parse(t *testing.T, provider core.CacheProvider) rueidis.ClientOption {
	t.Helper()

	options, _, err := parseOptions(provider, zap.NewNop().Sugar())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return options
}

// The uuid must match the one souin computes, which defaults the client name to
// souin-redis. See https://github.com/darkweak/storages/issues/64.
func TestParseOptions_DefaultsTheClientName(t *testing.T) {
	providers := map[string]core.CacheProvider{
		"url": {URL: souinAddress},
		"configuration": {Configuration: map[string]interface{}{
			initAddress: []interface{}{souinAddress},
			"Password":  "secret",
		}},
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			options := parse(t, provider)
			if options.ClientName != defaultClientName {
				t.Errorf("expected the client name %s, got %q", defaultClientName, options.ClientName)
			}

			uuid := (&Redis{configuration: options}).Uuid()
			if expected := souinAddress + "--0-souin-redis-0s"; uuid != expected {
				t.Errorf("expected the uuid %s, got %s", expected, uuid)
			}
		})
	}
}

func TestParseOptions_KeepsTheGivenClientName(t *testing.T) {
	options := parse(t, core.CacheProvider{Configuration: map[string]interface{}{
		initAddress:  []interface{}{souinAddress},
		"ClientName": "custom",
	}})

	if options.ClientName != "custom" {
		t.Errorf("expected the client name custom, got %q", options.ClientName)
	}
}

// Caddyfile dispensers tokenize every scalar as a string.
func TestParseOptions_CoercesTheCaddyfileScalars(t *testing.T) {
	configuration := map[string]interface{}{
		initAddress:             "redis-1:6379, redis-2:6379",
		"SelectDB":              "2",
		"ConnWriteTimeout":      "5s",
		"MaxFlushDelay":         "100",
		"DisableCache":          "true",
		"BlockingPoolSize":      "12",
		"ClientSetInfo":         "lib,1.0",
		"Dialer":                map[string]interface{}{"Timeout": "3s", "KeepAlive": "1m"},
		"Sentinel":              map[string]interface{}{"MasterSet": "main", "Dialer": map[string]interface{}{"Timeout": "2s"}},
		"AlwaysRESP2":           "not-a-bool",
		"HashTag":               "{souin}",
		"ClientTrackingOptions": []interface{}{"PREFIX", "a"},
	}

	options, hashtags, err := parseOptions(core.CacheProvider{Configuration: configuration}, zap.NewNop().Sugar())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(options.InitAddress, []string{"redis-1:6379", "redis-2:6379"}) {
		t.Errorf("unexpected InitAddress %v", options.InitAddress)
	}

	if options.SelectDB != 2 || options.BlockingPoolSize != 12 {
		t.Errorf("unexpected SelectDB %d or BlockingPoolSize %d", options.SelectDB, options.BlockingPoolSize)
	}

	if options.ConnWriteTimeout != 5*time.Second || options.MaxFlushDelay != 100 {
		t.Errorf("unexpected ConnWriteTimeout %s or MaxFlushDelay %s", options.ConnWriteTimeout, options.MaxFlushDelay)
	}

	if !options.DisableCache {
		t.Error("expected DisableCache to be true")
	}

	if options.AlwaysRESP2 {
		t.Error("an unparsable value must not enable AlwaysRESP2")
	}

	if !slices.Equal(options.ClientSetInfo, []string{"lib", "1.0"}) {
		t.Errorf("unexpected ClientSetInfo %v", options.ClientSetInfo)
	}

	if !slices.Equal(options.ClientTrackingOptions, []string{"PREFIX", "a"}) {
		t.Errorf("unexpected ClientTrackingOptions %v", options.ClientTrackingOptions)
	}

	if options.Dialer.Timeout != 3*time.Second || options.Dialer.KeepAlive != time.Minute {
		t.Errorf("unexpected Dialer %+v", options.Dialer)
	}

	if options.Sentinel.MasterSet != "main" || options.Sentinel.Dialer.Timeout != 2*time.Second {
		t.Errorf("unexpected Sentinel %+v", options.Sentinel)
	}

	if hashtags != "{souin}" {
		t.Errorf("unexpected hashtags %s", hashtags)
	}

	// The caller configuration must be left untouched.
	if configuration["SelectDB"] != "2" {
		t.Error("the given configuration has been mutated")
	}
}

func TestParseOptions_RequiresAnAddress(t *testing.T) {
	if _, _, err := parseOptions(core.CacheProvider{Configuration: map[string]interface{}{"Password": "secret"}}, zap.NewNop().Sugar()); err == nil {
		t.Error("expected an error without any address")
	}
}

// Every scalar or string list option rueidis exposes must be coerced, otherwise
// setting it from a Caddyfile silently drops it. This fails when a rueidis
// upgrade adds such an option, so it gets listed in the sanitizer.
func TestSanitizedFieldsCoverClientOption(t *testing.T) {
	durationType := reflect.TypeOf(time.Duration(0))
	options := reflect.TypeOf(rueidis.ClientOption{})

	for i := range options.NumField() {
		field := options.Field(i)
		if !field.IsExported() {
			continue
		}

		var list []string

		switch {
		case field.Type == durationType:
			list = durationFields
		case field.Type.Kind() == reflect.Bool:
			list = boolFields
		case field.Type.Kind() >= reflect.Int && field.Type.Kind() <= reflect.Uint64:
			list = intFields
		case field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.String:
			list = listFields
		default:
			continue
		}

		if !slices.Contains(list, field.Name) {
			t.Errorf("rueidis.ClientOption.%s (%s) is not coerced by sanitizeProperties", field.Name, field.Type)
		}
	}
}
