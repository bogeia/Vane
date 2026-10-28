package config

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"

	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/biz/watchwords"
)

type Config struct {
	Push        Push      `mapstructure:"push"`
	Selection   Selection `mapstructure:"selection"`
	Server      Server    `mapstructure:"server"`
	Log         Log       `mapstructure:"log"`
	Database    Database  `mapstructure:"database"`
	Site        Site      `mapstructure:"site"`
	Fetch       Fetch     `mapstructure:"fetch"`
	LLM         LLM       `mapstructure:"llm"`
	Collect     Collect   `mapstructure:"collect"`
	Worker      Worker    `mapstructure:"worker"`
	Web         Web       `mapstructure:"web"`
	SourcesFile string    `mapstructure:"sources_file" validate:"notblank"`
	Sources     []Source
}

type Push struct {
	RequestTimeout time.Duration `mapstructure:"request_timeout" validate:"gt=0"`
	Webhooks       []Webhook     `mapstructure:"webhooks" validate:"unique=Name,dive"`
}

const (
	WebhookTypeWecom  = "wecom"
	WebhookTypeFeishu = "feishu"
)

type Realtime struct {
	Enabled bool       `mapstructure:"enabled"`
	Scope   string     `mapstructure:"scope" validate:"oneof=selected all"`
	Topics  []string   `mapstructure:"topics"`
	Words   [][]string `mapstructure:"words"`
}

type Webhook struct {
	Name     string   `mapstructure:"name" validate:"notblank"`
	Daily    bool     `mapstructure:"daily"`
	Alerts   bool     `mapstructure:"alerts"`
	Realtime Realtime `mapstructure:"realtime"`
	Type     string   `mapstructure:"type" validate:"oneof=feishu wecom"`
	URL      string   `mapstructure:"url" validate:"absolute_http_url"`
	Secret   string   `mapstructure:"secret" validate:"excluded_if=Type wecom"`
}

type Selection struct {
	Thresholds Thresholds `mapstructure:"thresholds"`
}

type Thresholds struct {
	Official int `mapstructure:"official" validate:"gte=0,lte=100"`
	Media    int `mapstructure:"media" validate:"gte=0,lte=100"`
}

type Database struct {
	URL             string        `mapstructure:"url" validate:"postgres_url"`
	MaxOpenConns    int           `mapstructure:"max_open_conns" validate:"gt=0,pool_capacity"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" validate:"gte=0,ltefield=MaxOpenConns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" validate:"gt=0"`
}
type Site struct {
	Name      string `mapstructure:"name" validate:"notblank"`
	PublicURL string `mapstructure:"public_url" validate:"absolute_http_url"`
}
type Retry struct {
	MaxAttempts     int           `mapstructure:"max_attempts" validate:"gte=1,lte=10"`
	InitialInterval time.Duration `mapstructure:"initial_interval" validate:"gt=0,lte=3600000000000"`
	MaxInterval     time.Duration `mapstructure:"max_interval" validate:"gtefield=InitialInterval,lte=3600000000000"`
}
type Fetch struct {
	ProxyURL       string        `mapstructure:"proxy_url" validate:"http_proxy_url"`
	RequestTimeout time.Duration `mapstructure:"request_timeout" validate:"gt=0,lte=300000000000"`
	Retry          Retry         `mapstructure:"retry"`
}
type LLM struct {
	RequestTimeout time.Duration `mapstructure:"request_timeout" validate:"gt=0,lte=300000000000"`
	Retry          Retry         `mapstructure:"retry"`
	Enabled        bool          `mapstructure:"enabled"`
	BaseURL        string        `mapstructure:"base_url"`
	APIKey         string        `mapstructure:"api_key"`
	Model          string        `mapstructure:"model"`
	Steps          LLMSteps      `mapstructure:"steps"`
}

// LLMStep overrides the shared connection and model for one processing step.
type LLMStep struct {
	BaseURL         string `mapstructure:"base_url"`
	APIKey          string `mapstructure:"api_key"`
	Model           string `mapstructure:"model"`
	ReasoningEffort string `mapstructure:"reasoning_effort"`
}
type LLMSteps struct {
	Prefilter LLMStep `mapstructure:"prefilter"`
	Score     LLMStep `mapstructure:"score"`
	Write     LLMStep `mapstructure:"write"`
	Group     LLMStep `mapstructure:"group"`
}

func (c LLM) Resolve(step LLMStep) LLMStep {
	step.BaseURL = cmp.Or(step.BaseURL, c.BaseURL)
	step.APIKey = cmp.Or(step.APIKey, c.APIKey)
	step.Model = cmp.Or(step.Model, c.Model)
	return step
}

type Collect struct {
	Enabled bool `mapstructure:"enabled"`
}
type Worker struct {
	AttemptTimeout time.Duration `mapstructure:"attempt_timeout" validate:"gt=0,lte=1800000000000"`
	Retry          Retry         `mapstructure:"retry"`
	Count          int           `mapstructure:"count" validate:"gt=0"`
}
type Web struct {
	Dir string `mapstructure:"dir" validate:"notblank"`
}
type WebList struct {
	Item  string `mapstructure:"item"`
	Link  string `mapstructure:"link"`
	Title string `mapstructure:"title"`
	Date  string `mapstructure:"date"`
}

type Source struct {
	IconURL         *string    `mapstructure:"icon_url" validate:"omitempty,absolute_http_url"`
	Kind            string     `mapstructure:"kind" validate:"oneof=rss web_list hf_models"`
	WebList         *WebList   `mapstructure:"web_list"`
	Block           [][]string `mapstructure:"block"`
	ID              string     `mapstructure:"id" validate:"notblank"`
	Name            string     `mapstructure:"name" validate:"notblank"`
	Tier            string     `validate:"oneof=official media"`
	FeedURL         string     `mapstructure:"feed_url" validate:"absolute_http_url"`
	Enabled         bool       `mapstructure:"enabled"`
	IntervalMinutes int        `mapstructure:"interval_minutes" validate:"gt=0,interval_minutes"`
}

type Server struct {
	RequestTimeout  time.Duration `mapstructure:"request_timeout" validate:"gt=0,ltfield=WriteTimeout"`
	Address         string        `mapstructure:"address" validate:"listen_address"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout" validate:"gt=0"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout" validate:"gt=0"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout" validate:"gt=0"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout" validate:"gt=0"`
}

type Log struct {
	Level      slog.Level `mapstructure:"level"`
	Console    bool       `mapstructure:"console"`
	Filename   string     `mapstructure:"filename" validate:"notblank"`
	MaxSize    int        `mapstructure:"max_size" validate:"gt=0"`
	MaxBackups int        `mapstructure:"max_backups" validate:"gte=0"`
	MaxAge     int        `mapstructure:"max_age" validate:"gte=0"`
	Compress   bool       `mapstructure:"compress"`
}

func Load(path string) (Config, error) { return load(path, false) }

// LoadEvaluation keeps the explicit schema while ignoring unused database values.
func LoadEvaluation(path string) (Config, error) { return load(path, true) }

func load(path string, evaluation bool) (Config, error) {
	var config Config
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".yaml" && ext != ".yml" {
		return config, fmt.Errorf("configuration must be a YAML file")
	}
	if err := decodeFile(path, &config, true); err != nil {
		return config, err
	}
	if strings.TrimSpace(config.SourcesFile) == "" {
		return config, fmt.Errorf("sources_file must not be blank")
	}
	sourcesPath := config.SourcesFile
	if !filepath.IsAbs(sourcesPath) {
		sourcesPath = filepath.Join(filepath.Dir(path), sourcesPath)
	}
	var sources struct {
		Sources struct {
			Official []Source `mapstructure:"official"`
			Media    []Source `mapstructure:"media"`
		} `mapstructure:"sources"`
	}
	if err := decodeFile(sourcesPath, &sources, false); err != nil {
		return config, fmt.Errorf("load sources file: %w", err)
	}
	seen := map[string]bool{}
	for _, group := range []struct {
		tier    string
		sources []Source
	}{
		{taxonomy.TierOfficial, sources.Sources.Official}, {taxonomy.TierMedia, sources.Sources.Media},
	} {
		for i, source := range group.sources {
			path := fmt.Sprintf("sources.%s[%d]", group.tier, i)
			source.Tier = group.tier
			if seen[source.ID] {
				return config, fmt.Errorf("%s.id failed unique", path)
			}
			if err := validateSource(source, path); err != nil {
				return config, err
			}
			seen[source.ID] = true
			config.Sources = append(config.Sources, source)
		}
	}
	if err := config.validate(evaluation); err != nil {
		return config, err
	}
	return config, nil
}

func decodeFile(path string, target any, main bool) error {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".yaml" && ext != ".yml" {
		return fmt.Errorf("configuration must be a YAML file")
	}
	content, err := os.ReadFile(path)
	var settings map[string]any
	if err != nil || yaml.Unmarshal(content, &settings) != nil {
		return fmt.Errorf("read configuration: file unavailable or invalid YAML")
	}
	v := viper.New()
	if err := v.MergeConfigMap(settings); err != nil {
		return fmt.Errorf("read configuration: file unavailable or invalid YAML")
	}
	if main {
		for _, path := range []string{"fetch.proxy_url", "push", "push.request_timeout", "push.webhooks", "selection", "selection.thresholds", "selection.thresholds.official", "selection.thresholds.media"} {
			if !v.InConfig(path) {
				return fmt.Errorf("%s failed required", path)
			}
		}
	}
	if main && (v.InConfig("sources") || slices.Contains(v.AllKeys(), "sources")) {
		return fmt.Errorf("inline sources is not supported; use sources_file")
	}
	if main {
		hooks, _ := v.Get("push.webhooks").([]any)
		for i, value := range hooks {
			hook, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for _, field := range []string{"name", "type", "url", "secret", "daily", "alerts", "realtime"} {
				if _, ok := hook[field]; !ok {
					return fmt.Errorf("push.webhooks[%d].%s failed required", i, field)
				}
			}
			if realtime, ok := hook["realtime"].(map[string]any); ok {
				for _, field := range []string{"enabled", "scope", "topics", "words"} {
					if _, ok := realtime[field]; !ok {
						return fmt.Errorf("push.webhooks[%d].realtime.%s failed required", i, field)
					}
				}
			}
		}
	}
	if !main {
		groups, ok := v.Get("sources").(map[string]any)
		if !ok {
			return fmt.Errorf("sources failed groups")
		}
		for name := range groups {
			if name != taxonomy.TierOfficial && name != taxonomy.TierMedia {
				return fmt.Errorf("sources failed known_group")
			}
		}
		for _, tier := range []string{taxonomy.TierOfficial, taxonomy.TierMedia} {
			value, present := groups[tier]
			if !present || value == nil {
				return fmt.Errorf("sources.%s failed required", tier)
			}
			sources, ok := value.([]any)
			if !ok {
				return fmt.Errorf("sources.%s failed list", tier)
			}
			for i, value := range sources {
				path := fmt.Sprintf("sources.%s[%d]", tier, i)
				source, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("%s failed object", path)
				}
				if _, present := source["tier"]; present {
					return fmt.Errorf("%s.tier failed excluded", path)
				}
				for _, field := range []string{"id", "kind", "name", "feed_url", "enabled", "interval_minutes", "block"} {
					if _, present := source[field]; !present {
						return fmt.Errorf("%s.%s failed required", path, field)
					}
				}
				if web, ok := source["web_list"].(map[string]any); ok {
					for _, field := range []string{"item", "link", "title", "date"} {
						if _, present := web[field]; !present {
							return fmt.Errorf("%s.web_list.%s failed required", path, field)
						}
					}
				}
			}
		}
	}
	// Decode the complete YAML map: Viper.AllSettings drops unknown empty maps and nulls.
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:               target,
		ErrorUnset:           true,
		ErrorUnused:          true,
		AllowUnsetPointer:    true,
		IgnoreUntaggedFields: true,
		WeaklyTypedInput:     false,
		DecodeHook: func(from, to reflect.Type, value any) (any, error) {
			if to == reflect.TypeFor[slog.Level]() {
				if from.Kind() != reflect.String {
					return nil, fmt.Errorf("log level must be a string")
				}
				var level slog.Level
				err := level.UnmarshalText([]byte(reflect.ValueOf(value).String()))
				return level, err
			}
			if to == reflect.TypeFor[time.Duration]() {
				if from.Kind() != reflect.String {
					return nil, fmt.Errorf("duration must be a string with units, such as 10s")
				}
				return time.ParseDuration(reflect.ValueOf(value).String())
			}
			if to.Kind() == reflect.Int && from.Kind() != reflect.Int {
				return nil, fmt.Errorf("expected integer, got %s", from)
			}
			return value, nil
		},
	})
	if err != nil || decoder.Decode(settings) != nil {
		return fmt.Errorf("decode configuration: invalid field types, unknown fields or missing required fields")
	}
	return rejectNull("config", settings)
}

func (c Config) validate(evaluation bool) error {
	var validationErr error
	if evaluation {
		validationErr = configValidator.StructExcept(c, "Database")
	} else {
		validationErr = configValidator.Struct(c)
	}
	if err := validationErr; err != nil {
		// Never format field values: configuration may contain database or model credentials.
		var fields validator.ValidationErrors
		if errors.As(err, &fields) {
			return fmt.Errorf("invalid configuration: %s failed %s", fields[0].Namespace(), fields[0].Tag())
		}
		return fmt.Errorf("invalid configuration")
	}

	for i, hook := range c.Push.Webhooks {
		if err := validateRealtime(hook.Realtime, i); err != nil {
			return err
		}
	}
	return nil
}

func validateSource(source Source, path string) error {
	if err := configValidator.Struct(source); err != nil {
		var fields validator.ValidationErrors
		if errors.As(err, &fields) {
			field, _ := reflect.TypeFor[Source]().FieldByName(fields[0].StructField())
			return fmt.Errorf("%s.%s failed %s", path, field.Tag.Get("mapstructure"), fields[0].Tag())
		}
		return fmt.Errorf("%s failed validation", path)
	}
	if source.Kind == "hf_models" && !isHFOrganizationURL(source.FeedURL) {
		return fmt.Errorf("%s.feed_url failed hf_organization_url", path)
	}
	if source.Kind == "web_list" {
		if source.WebList == nil {
			return fmt.Errorf("%s.web_list failed required", path)
		}
		for _, field := range []struct{ name, selector string }{{"item", source.WebList.Item}, {"link", source.WebList.Link}, {"title", source.WebList.Title}, {"date", source.WebList.Date}} {
			if strings.TrimSpace(field.selector) == "" {
				return fmt.Errorf("%s.web_list.%s failed notblank", path, field.name)
			}
			if _, err := cascadia.Compile(field.selector); err != nil {
				return fmt.Errorf("%s.web_list.%s failed css_selector", path, field.name)
			}
		}
	} else if source.WebList != nil {
		return fmt.Errorf("%s.web_list failed excluded", path)
	}
	if err := watchwords.ValidateWords(source.Block); err != nil {
		return fmt.Errorf("%s.%s", path, strings.Replace(err.Error(), "words[", "block[", 1))
	}
	return nil
}

func validateRealtime(r Realtime, i int) error {
	for _, slug := range r.Topics {
		if !taxonomy.ValidTopic(slug) {
			return fmt.Errorf("push.webhooks[%d].realtime.topics failed known_topic", i)
		}
	}
	if err := watchwords.ValidateWords(r.Words); err != nil {
		return fmt.Errorf("push.webhooks[%d].realtime.%w", i, err)
	}
	if r.Enabled && r.Scope == taxonomy.ScopeAll && len(r.Topics) == 0 && len(r.Words) == 0 {
		return fmt.Errorf("push.webhooks[%d].realtime failed requires_filter", i)
	}
	return nil
}

var configValidator = newConfigValidator()

func newConfigValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterStructValidation(func(sl validator.StructLevel) {
		c := sl.Current().Interface().(LLM)
		if !c.Enabled {
			return
		}
		for _, entry := range []struct {
			name string
			step LLMStep
		}{
			{"Prefilter", c.Steps.Prefilter}, {"Score", c.Steps.Score}, {"Write", c.Steps.Write}, {"Group", c.Steps.Group},
		} {
			step := c.Resolve(entry.step)
			if !isHTTPURL(step.BaseURL) {
				sl.ReportError(step.BaseURL, "Steps."+entry.name+".BaseURL", "", "absolute_http_url", "")
			}
			if strings.TrimSpace(step.APIKey) == "" {
				sl.ReportError(step.APIKey, "Steps."+entry.name+".APIKey", "", "notblank", "")
			}
			if strings.TrimSpace(step.Model) == "" {
				sl.ReportError(step.Model, "Steps."+entry.name+".Model", "", "notblank", "")
			}
		}
	}, LLM{})
	rules := map[string]validator.Func{
		"http_proxy_url": func(f validator.FieldLevel) bool {
			value := f.Field().String()
			if value == "" {
				return true
			}
			u, err := url.Parse(value)
			if err != nil || u.Scheme != "http" || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(value, "#") {
				return false
			}
			_, port, err := net.SplitHostPort(u.Host)
			n, parseErr := strconv.Atoi(port)
			return err == nil && parseErr == nil && n >= 1 && n <= 65535
		},
		"pool_capacity": func(f validator.FieldLevel) bool {
			c, ok := f.Top().Interface().(Config)
			if !ok {
				return false
			}
			return !c.LLM.Enabled || f.Field().Int() > int64(c.Worker.Count)
		},
		"notblank":          func(f validator.FieldLevel) bool { return strings.TrimSpace(f.Field().String()) != "" },
		"absolute_http_url": func(f validator.FieldLevel) bool { return isHTTPURL(f.Field().String()) },
		"postgres_url": func(f validator.FieldLevel) bool {
			u, err := url.Parse(f.Field().String())
			return err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") && u.Host != "" && strings.Trim(u.Path, "/") != ""
		},
		"listen_address": func(f validator.FieldLevel) bool {
			_, port, err := net.SplitHostPort(f.Field().String())
			if err != nil {
				return false
			}
			n, err := strconv.Atoi(port)
			return err == nil && n >= 1 && n <= 65535
		},
		"interval_minutes": func(f validator.FieldLevel) bool { return f.Field().Int() <= int64((1<<63-1)/time.Minute) },
	}
	for name, rule := range rules {
		if err := v.RegisterValidation(name, rule); err != nil {
			panic(err)
		}
	}
	return v
}

func isHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

func rejectNull(path string, value any) error {
	switch value := value.(type) {
	case nil:
		return fmt.Errorf("%s must not be null", path)
	case map[string]any:
		for key, child := range value {
			if err := rejectNull(path+"."+key, child); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range value {
			if err := rejectNull(fmt.Sprintf("%s[%d]", path, i), child); err != nil {
				return err
			}
		}
	}
	return nil
}

func isHFOrganizationURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "huggingface.co" || parsed.User != nil {
		return false
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(value, "#") || parsed.RawPath != "" {
		return false
	}
	org, found := strings.CutPrefix(parsed.Path, "/")
	return found && org != "" && org != "." && org != ".." && !strings.Contains(org, "/")
}
