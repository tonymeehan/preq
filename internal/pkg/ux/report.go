package ux

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/prequel-dev/preq/internal/pkg/matchz"
	"github.com/prequel-dev/prequel-compiler/pkg/parser"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/rs/zerolog/log"
)

var (
	ErrInvalidSeverity = errors.New("invalid severity")
)

const (
	sevCritical   = "critical"
	sevHigh       = "high"
	sevMedium     = "medium"
	sevLow        = "low"
	sevInfo       = "info"
	colorCritical = text.FgHiRed
	colorHigh     = text.FgHiYellow
	colorMedium   = text.FgHiMagenta
	colorLow      = text.FgHiGreen
	colorInfo     = text.FgHiBlue
	reportFmt     = "preq-report-%d.json"
)

var (
	sevWidth = max(len(sevCritical), len(sevHigh), len(sevMedium), len(sevLow), len(sevInfo))
)

type ReportT struct {
	mux     sync.Mutex
	CreHits map[string][]time.Time
	Hits    map[string]map[time.Time]matchz.HitsT
	Rules   map[string]parser.ParseRuleT
	Pw      progress.Writer
}

func NewReport(pw progress.Writer) *ReportT {
	return &ReportT{
		CreHits: make(map[string][]time.Time),                // cre -> timestamps for each detection
		Hits:    make(map[string]map[time.Time]matchz.HitsT), // cre -> timestamp -> matchz.HitsT
		Rules:   make(map[string]parser.ParseRuleT),          // cre -> parser.ParseRuleT
		Pw:      pw,
	}
}

func (r *ReportT) AddCreHit(cre *parser.ParseCreT, hit time.Time, m matchz.HitsT) bool {
	r.mux.Lock()
	defer r.mux.Unlock()

	var newDetection bool

	if _, ok := r.CreHits[cre.Id]; !ok {
		newDetection = true
	}

	r.CreHits[cre.Id] = append(r.CreHits[cre.Id], hit)

	if _, ok := r.Hits[cre.Id]; !ok {
		r.Hits[cre.Id] = make(map[time.Time]matchz.HitsT)
	}

	r.Hits[cre.Id][hit] = m

	return newDetection
}

func (r *ReportT) AddRules(rules *parser.RulesT) {
	r.mux.Lock()
	defer r.mux.Unlock()

	var ok bool
	for _, rule := range rules.Rules {
		if _, ok = r.Rules[rule.Cre.Id]; !ok {
			r.Rules[rule.Cre.Id] = rule
		} else {
			log.Warn().Str("creId", rule.Cre.Id).Msg("CRE already exists")
		}
	}
}

func (r *ReportT) GetCre(creId string) parser.ParseRuleT {
	r.mux.Lock()
	defer r.mux.Unlock()
	return r.Rules[creId]
}

func getColorizedCount(c int, timestamp time.Time) string {
	count := text.Colors{text.FgBlue, text.Bold}.Sprintf("[%d hits ", c)
	count += text.Colors{text.FgMagenta, text.Bold}.Sprintf("@ ")
	count += text.Colors{text.FgBlue, text.Bold}.Sprintf("%s]", timestamp.Format(time.RFC3339Nano))
	return count
}

func getColorizedCre(creId string, colors text.Colors) string {
	return colors.Sprintf("%-20s", creId)
}

type severityT struct {
	severity string
	color    text.Color
}

func getSeverity(severity uint) (*severityT, error) {
	switch severity {
	case parser.SeverityCritical:
		return &severityT{
			severity: sevCritical,
			color:    colorCritical,
		}, nil
	case parser.SeverityHigh:
		return &severityT{
			severity: sevHigh,
			color:    colorHigh,
		}, nil
	case parser.SeverityMedium:
		return &severityT{
			severity: sevMedium,
			color:    colorMedium,
		}, nil
	case parser.SeverityLow:
		return &severityT{
			severity: sevLow,
			color:    colorLow,
		}, nil
	case parser.SeverityInfo:
		return &severityT{
			severity: sevInfo,
			color:    colorInfo,
		}, nil
	}

	return nil, ErrInvalidSeverity
}

func (r *ReportT) DisplayCREs() error {
	r.mux.Lock()
	defer r.mux.Unlock()

	var (
		rules = make([]parser.ParseRuleT, 0)
	)

	for _, rule := range r.Rules {
		rules = append(rules, rule)
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Cre.Severity > rules[j].Cre.Severity
	})

	for _, rule := range rules {

		var (
			creHits = r.CreHits[rule.Cre.Id]
		)

		if len(creHits) == 0 {
			continue
		}

		sev, err := getSeverity(rule.Cre.Severity)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get severity")
			continue
		}

		var (
			count = getColorizedCount(len(creHits), creHits[0])
			cre   = getColorizedCre(rule.Cre.Id, text.Colors{sev.color, text.Bold})
			tmpl  = fmt.Sprintf("%%%ds", sevWidth)
			sevS  = text.Colors{sev.color}.Sprintf(tmpl, sev.severity)
		)

		r.Pw.Log(fmt.Sprintf("%s %s %s", cre, sevS, count))
	}
	return nil
}

func (r *ReportT) Write(path string) (string, error) {
	r.mux.Lock()
	defer r.mux.Unlock()

	var (
		reportName string
		o          any
		data       []byte
		err        error
	)

	if path == "" {
		reportName = fmt.Sprintf(reportFmt, time.Now().Unix())
	} else {
		reportName = path
	}

	if o, err = r.createReport(); err != nil {
		return "", err
	}

	data, err = json.MarshalIndent(o, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal report")
		return "", err
	}

	if err = os.WriteFile(reportName, data, 0644); err != nil {
		return "", err
	}

	return reportName, nil
}

func (r *ReportT) PrintReport() error {
	r.mux.Lock()
	defer r.mux.Unlock()

	var (
		o    any
		data []byte
		err  error
	)

	if o, err = r.createReport(); err != nil {
		return err
	}

	data, err = json.MarshalIndent(o, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal report")
		return err
	}

	fmt.Fprintln(os.Stdout, string(data))

	return nil
}

func (r *ReportT) Size() int {
	r.mux.Lock()
	defer r.mux.Unlock()
	return len(r.CreHits)
}

type ReportDocT []map[string]any

func (r *ReportT) CreateReport() (ReportDocT, error) {
	r.mux.Lock()
	defer r.mux.Unlock()
	return r.createReport()
}

func (r *ReportT) createReport() (ReportDocT, error) {
	var (
		out = make([]map[string]any, 0)
	)

	// timestamp, CRE, rule id and hash, hit data
	for id, creHits := range r.CreHits {

		var o = make(map[string]any)
		o["timestamp"] = creHits[0].Format(time.RFC3339Nano)
		o["id"] = id
		o["cre"] = r.Rules[id].Cre
		o["rule_id"] = r.Rules[id].Metadata.Id
		o["rule_hash"] = r.Rules[id].Metadata.Hash

		type entryT struct {
			Timestamp time.Time `json:"timestamp"`
			Entry     string    `json:"entry"`
		}
		matchHits := make([]entryT, 0)
		for _, hit := range creHits {

			for _, e := range r.Hits[id][hit].Entries {
				matchHits = append(matchHits, entryT{
					Timestamp: time.Unix(0, e.Timestamp),
					Entry:     string(e.Entry),
				})
			}
		}

		o["hits"] = matchHits
		out = append(out, o)
	}

	return out, nil
}
