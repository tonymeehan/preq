package utils

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/prequel-dev/prequel-compiler/pkg/parser"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

var (
	ErrGzip  = errors.New("gzip error")
	ErrRead  = errors.New("read error")
	ErrWrite = errors.New("write error")
)

func ParseTime(tstr, def string) (ts int64, err error) {
	tstr = strings.TrimSpace(tstr)
	if tstr == "" {
		tstr = def
	}

	now := time.Now()

	switch tstr {
	case "now":
		ts = now.UnixNano()
	case "-infinity", "-inf", "-∞", "0":
		ts = 0
	case "infinity", "+inf", "inf", "∞", "+":
		ts = math.MaxInt64
	default:
		if d, err := time.ParseDuration(tstr); err == nil {
			ts = now.Add(d).UnixNano()
		} else if stamp, err := time.Parse(time.RFC3339, tstr); err == nil {
			ts = stamp.UnixNano()
		} else {
			return 0, fmt.Errorf("fail parse timestamp: %w", err)
		}
	}

	return ts, nil
}

func GetOSInfo() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}

func OpenRulesFile(filePath string) (io.Reader, func(), error) {

	var (
		file *os.File
		buf  [2]byte
		err  error
	)

	if file, err = os.Open(filePath); err != nil {
		return nil, nil, err
	}

	cleanup := func() { file.Close() }

	if _, err = file.Read(buf[:]); err != nil {
		file.Close()
		return nil, nil, err
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, nil, err
	}

	if buf[0] == 0x1f && buf[1] == 0x8b {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			return nil, nil, err
		}
		cleanup = func() {
			gzReader.Close()
			file.Close()
		}
		return gzReader, cleanup, nil
	}

	return file, cleanup, nil
}

func ParseRulesPath(path string) (*parser.RulesT, error) {
	var (
		reader io.Reader
		close  func()
		err    error
	)

	if reader, close, err = OpenRulesFile(path); err != nil {
		return nil, err
	}
	defer close()

	return ParseRules(reader)
}

func ParseRules(rdr io.Reader) (*parser.RulesT, error) {
	var (
		dupes  = make(map[string]struct{})
		config = &parser.RulesT{
			Rules: make([]parser.ParseRuleT, 0),
			Terms: make(map[string]parser.ParseTermT),
		}
		decoder *yaml.Decoder
		err     error
	)

	decoder = yaml.NewDecoder(rdr)

LOOP:
	for {

		var sections map[string]any
		err = decoder.Decode(&sections)

		switch err {
		case nil:
		case io.EOF:
			break LOOP
		default:
			log.Error().Err(err).Msg("Fail yaml unmarshal rules package")
			return nil, err
		}

		for name, section := range sections {

			switch name {
			case "rules":

				var (
					rules []parser.ParseRuleT
					b     []byte
					ok    bool
				)

				if b, err = yaml.Marshal(section); err != nil {
					return nil, err
				}

				if err = yaml.Unmarshal(b, &rules); err != nil {
					return nil, err
				}

				for _, rule := range rules {

					if _, ok = dupes[rule.Metadata.Hash]; ok {
						log.Error().Str("id", rule.Metadata.Hash).Msg("Duplicate rule hash id. Aborting...")
						return nil, fmt.Errorf("duplicate rule hash id=%s cre=%s", rule.Metadata.Hash, rule.Cre.Id)
					}

					if _, ok = dupes[rule.Metadata.Id]; ok {
						log.Error().Str("id", rule.Metadata.Id).Msg("Duplicate rule id. Aborting...")
						return nil, fmt.Errorf("duplicate rule id=%s cre=%s", rule.Metadata.Id, rule.Cre.Id)
					}

					if _, ok = dupes[rule.Cre.Id]; ok {
						log.Error().Str("id", rule.Cre.Id).Msg("Duplicate rule cre id. Aborting...")
						return nil, fmt.Errorf("duplicate rule cre id=%s cre=%s", rule.Cre.Id, rule.Cre.Id)
					}

					dupes[rule.Metadata.Hash] = struct{}{}
					dupes[rule.Metadata.Id] = struct{}{}
					dupes[rule.Cre.Id] = struct{}{}
				}

				config.Rules = rules
				dupes = nil

			case "terms":

				if config.Terms, err = parseTerms(section); err != nil {
					return nil, err
				}

			default:
				// skip
			}
		}
	}

	return config, nil
}

func parseTerms(section any) (map[string]parser.ParseTermT, error) {
	var err error

	switch s := section.(type) {
	case map[string]any:
		log.Info().Int("terms", len(s)).Msg("Parsing terms section")

		var (
			terms map[string]parser.ParseTermT
			b     []byte
		)

		if b, err = yaml.Marshal(s); err != nil {
			return nil, err
		}

		if err = yaml.Unmarshal(b, &terms); err != nil {
			return nil, err
		}

		return terms, nil

	default:
		log.Error().Any("section", section).Msg("Invalid terms section")
		return nil, fmt.Errorf("invalid terms section")
	}

}

func GunzipBytes(path string) ([]byte, error) {

	var (
		compressedData []byte
		gzReader       *gzip.Reader
		decompressed   bytes.Buffer
		err            error
	)

	if compressedData, err = os.ReadFile(path); err != nil {
		return nil, ErrRead
	}

	if gzReader, err = gzip.NewReader(bytes.NewReader(compressedData)); err != nil {
		return nil, ErrGzip
	}
	defer gzReader.Close()

	if _, err = io.Copy(&decompressed, gzReader); err != nil {
		return nil, ErrWrite
	}

	return decompressed.Bytes(), nil
}

func Sha256Sum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	tmp := fmt.Sprintf("%s.tmp", dst)

	dstFile, err := os.Create(tmp)
	if err != nil {
		return err
	}

	// Copy file
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		return err
	}

	// Close dst file before rename to avoid permissions problems on Windows
	err = dstFile.Close()
	if err != nil {
		return err
	}

	// Copy permissions from source to destination
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	err = os.Chmod(tmp, srcInfo.Mode())
	if err != nil {
		return err
	}

	err = os.Rename(tmp, dst)
	if err != nil {
		return err
	}

	return nil
}

func UrlBase(fullUrl string) (string, error) {
	u, err := url.Parse(fullUrl)
	if err != nil {
		return "", err
	}
	return filepath.Base(u.Path), nil
}
