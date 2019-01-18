package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"

	kitlog "github.com/go-kit/kit/log"

	"github.com/hikhvar/journaldtail/pkg/storage"

	"github.com/coreos/go-systemd/sdjournal"
	"github.com/grafana/loki/pkg/promtail"
	"github.com/hikhvar/journaldtail/pkg/journald"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

var lokiHostURL = "http://localhost:3100/api/prom/push"
var debug = false

func main() {
	fmt.Printf("START\n")
	log.Printf("STARTed\n")

	var logger kitlog.Logger
	logger = kitlog.NewLogfmtLogger(kitlog.NewSyncWriter(os.Stderr))
	log.SetOutput(kitlog.NewStdlibAdapter(logger))
	// TODO: Store state on disk
	memStorage := storage.Memory{}
	journal, err := sdjournal.NewJournal()
	if err != nil {
		log.Fatal(fmt.Sprintf("could not open journal: %s", err.Error()))
	}
	reader := journald.NewReader(journal, &memStorage)

	// TODO: Read from CLI
	if v, isSet := os.LookupEnv("LOKI_URL"); isSet {
		lokiHostURL = v
	}
	if v, isSet := os.LookupEnv("DEBUG"); isSet {
		if strings.ToLower(v) == "true" {
			debug = true
		}
	}
	fmt.Printf("DEBUG set to %v\n", debug)
	log.Printf("DEBUG set to %v\n", debug)

	cfg := promtail.ClientConfig{
		URL: flagext.URLValue{
			URL: MustParseURL(lokiHostURL),
		},
	}
	lokiClient, err := promtail.NewClient(cfg, logger)
	if err != nil {
		log.Fatal(fmt.Sprintf("could not create loki client: %s", err.Error()))
	}
	err = TailLoop(reader, lokiClient)
	if err != nil {
		log.Fatal(fmt.Sprintf("failed to tail journald: %s", err.Error()))
	}
}

// RFC3339NanoFixed is time.RFC3339Nano with nanoseconds padded using zeros to
const RFC3339NanoFixed = "2006-01-02T15:04:05.000000000Z07:00"

func TailLoop(reader *journald.Reader, writer *promtail.Client) error {
	var lastTS time.Time
	for {
		r, err := reader.Next()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if r != nil {
			ls := ToLabelSet(r)
			//ts := journald.ToGolangTime(r.RealtimeTimestamp)
			ts := journald.ToGolangTime(r.MonotonicTimestamp)
			msg := r.Fields[sdjournal.SD_JOURNAL_FIELD_MESSAGE]

			if !ts.After(lastTS) { // can't do "same time either"
				log.Fatal(fmt.Sprintf("%s is before %s! Message: %s", ts, lastTS, msg))
			}
			//fmt.Printf("\nDEBUGSVEN - (%s)\n", ts.Format(RFC3339NanoFixed))
			if debug {
				//fmt.Printf("\nDEBUG - (%s) %s (%s)\n", ts.Format(RFC3339NanoFixed), msg, ts.Format(RFC3339NanoFixed))
				fmt.Log(msg)
			}
			lastTS = ts
			err = writer.Handle(ls, ts, msg)
			if err != nil {
				return errors.Wrap(err, "could not enque systemd logentry")
			}
		}

	}
}

func ToLabelSet(reader *sdjournal.JournalEntry) model.LabelSet {
	ret := make(model.LabelSet)
	for key, value := range reader.Fields {
		if key != sdjournal.SD_JOURNAL_FIELD_MESSAGE {
			ret[model.LabelName(key)] = model.LabelValue(value)
		}
	}
	return ret
}

func MustParseURL(input string) *url.URL {
	u, err := url.Parse(input)
	if err != nil {
		panic(fmt.Sprintf("could not parse static url: %s", input))
	}
	return u
}
