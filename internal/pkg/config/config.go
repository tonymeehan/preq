package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prequel-dev/preq/internal/pkg/resolve"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

var (
	DefaultConfig = `timestamps:
    # {"level":"error","error":"context deadline exceeded","time":1744570895480541,"caller":"server.go:462"}
  - format: epochany
    pattern: |
      "time":(\d{16,19})
    # 2006-01-02T15:04:05Z07:00 log message
  - format: rfc3339
    pattern: |
      ^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+\-]\d{2}:\d{2}))
    # 2006/01/02 03:04:05 log message
  - format: "2006/01/02 03:04:05"
    pattern: |
      ^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})
    # 2006-01-02 15:04:05.000 log message
  - format: "2006-01-02 15:04:05.000" # ISO 8601
    pattern: |
      ^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})
    # Jan 2 15:04:05 log message
  - format: "Jan 2 15:04:05" # RFC 3164
    pattern: |
      ^([A-Z][a-z]{2}\s{1,2}\d{1,2}\s\d{2}:\d{2}:\d{2})
    # 2006-01-02 15:04:05 log message
  - format: "2006-01-02 15:04:05" # w3c
    pattern: |
      ^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})
    # I0102 15:04:05.000000 log message
  - format: "0102 15:04:05.000000" # go/klog
    pattern: |
      ^[IWEF](\d{4} \d{2}:\d{2}:\d{2}\.\d{6})
    # [2006-01-02 15:04:05,000] log message
  - format: "2006-01-02 15:04:05,000"
    pattern: |
      ^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3})\]
    # 2006-01-02 15:04:05.000000-0700 log message
  - format: "2006-01-02 15:04:05.000000-0700"
    pattern: |
      ^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{6}[+-]\d{4})
    # 2006/01/02 15:04:05 log message
  - format: "2006/01/02 15:04:05"
    pattern: |
      ^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})
    # 01/02/2006, 15:04:05 log message
  - format: "01/02/2006, 15:04:05" # IIS format
    pattern: |
      ^(\d{2}/\d{2}/\d{4}, \d{2}:\d{2}:\d{2})
    # 02 Jan 2006 15:04:05.000 log message
  - format: "02 Jan 2006 15:04:05.000" 
    pattern: |
      ^(\d{2} [A-Z][a-z]{2} \d{4} \d{2}:\d{2}:\d{2}\.\d{3})
    # 2006 Jan 02 15:04:05.000 log message
  - format: "2006 Jan 02 15:04:05.000"
    pattern: |
      ^(\d{4} [A-Z][a-z]{2} \d{2} \d{2}:\d{2}:\d{2}\.\d{3})
    # 02/Jan/2006:15:04:05.000 log message
  - format: "02/Jan/2006:15:04:05.000"
    pattern: |
      ^(\d{2}/[A-Z][a-z]{2}/\d{4}:\d{2}:\d{2}:\d{2}\.\d{3})
    # 01/02/2006 03:04:05 PM log message
  - format: "01/02/2006 03:04:05 PM"
    pattern: |
      ^(\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2} (AM|PM))
    # 2006 Jan 02 15:04:05 log message
  - format: "2006 Jan 02 15:04:05" 
    pattern: |
      ^(\d{4} [A-Z][a-z]{2} \d{2} \d{2}:\d{2}:\d{2})
    # 2006-01-02 15:04:05.000 log message
  - format: "2006-01-02 15:04:05.000"
    pattern: |
      ^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})
    # {"Id":19,"Version":1,"Opcode":13,"RecordId":1493,"LogName":"System","ProcessId":4324,"ThreadId":10456,"MachineName":"windows","TimeCreated":"\/Date(1743448267142)\/"}
  - format: epochany # Windows Get-Events JSON output
    pattern: |
      /Date\((\d+)\)
`
)

type Config struct {
	TimestampRegexes []Regex        `yaml:"timestamps"`
	Rules            Rules          `yaml:"rules"`
	UpdateFrequency  *time.Duration `yaml:"updateFrequency"`
	RulesVersion     string         `yaml:"rulesVersion"`
	AcceptUpdates    bool           `yaml:"acceptUpdates"`
	DataSources      string         `yaml:"dataSources"`
}

type Rules struct {
	Paths    []string `yaml:"paths"`
	Disabled bool     `yaml:"disableCommunityRules"`
}

type Regex struct {
	Pattern string `yaml:"pattern"`
	Format  string `yaml:"format"`
}

func LoadConfig(dir, file string) (*Config, error) {
	var config Config

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	if _, err := os.Stat(filepath.Join(dir, file)); os.IsNotExist(err) {
		if err := WriteDefaultConfig(filepath.Join(dir, file)); err != nil {
			log.Error().Err(err).Msg("Failed to write default config")
			return nil, err
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func WriteDefaultConfig(path string) error {
	return os.WriteFile(path, []byte(DefaultConfig), 0644)
}

func LoadConfigFromBytes(data string) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal([]byte(data), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c *Config) ResolveOpts() (opts []resolve.OptT) {

	if len(c.TimestampRegexes) > 0 {
		var specs []resolve.FmtSpec
		for _, r := range c.TimestampRegexes {
			specs = append(specs, resolve.FmtSpec{
				Pattern: strings.TrimSpace(r.Pattern),
				Format:  resolve.TimestampFmt(strings.TrimSpace(r.Format)),
			})
		}
		opts = append(opts, resolve.WithStampRegex(specs...))
	}

	return

}
