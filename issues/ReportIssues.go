package issues

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/go-resty/resty/v2"
	"github.com/sanity-io/litter"
)

// Options defines configuration options for ReportIssues.
type Options struct {
	Live   bool
	Folder string
	Server string

	MinimumInterval time.Duration
	Output          bool
	Debug           bool
}

// defaultOptions defines the default configuration.
var defaultOptions = Options{
	Live:            false,
	Folder:          "/var/coadmin",
	Server:          "http://127.0.0.1:3000/api",
	MinimumInterval: 60 * time.Second,
	Output:          false,
	Debug:           false,
}

type ReportSubmission struct {
	Issue Report `json:"issue"`
}

// Report represents a generated issue report.
type Report struct {
	Version     int                    `json:"v"`
	IssueID     uint32                 `json:"issue_id"`
	Meta        map[string]string      `json:"meta"`
	Options     map[string]interface{} `json:"options"`
	Caller      string                 `json:"caller"`
	StackTrace  []string               `json:"stackTrace"` // Not implemented for now.
	App         string                 `json:"app"`
	Extra       map[string]interface{} `json:"extra"`
	Description string                 `json:"description"`
	Level       string                 `json:"level"`
	LibVersion  string                 `json:"libversion"`
	T           int64                  `json:"t"`
}

// ReportIssues provides methods to generate and report issues.
type ReportIssues struct {
	AppName     string
	Options     Options
	reported    map[uint32]time.Time // stores next allowed reporting time per issue hash
	Meta        map[string]string
	Buffer      []Report
	Mutex       sync.Mutex    // protects reported map and Buffer
	restyClient *resty.Client // Resty client for HTTP requests
}

// NewReportIssues creates a new ReportIssues instance.
func NewReportIssues(appName string, options *Options) *ReportIssues {
	opts := defaultOptions
	if options != nil {
		// Override defaults with provided options.
		opts = *options
	}
	ri := &ReportIssues{
		AppName:  strings.ToLower(appName),
		Options:  opts,
		reported: make(map[uint32]time.Time),
		Meta: map[string]string{
			"hostname": getHostname(),
		},
		Buffer:      []Report{},
		restyClient: resty.New(),
	}
	if ri.Options.Live {
		ri.LogDebug("Initialized Resty client for HTTP requests")
		// Start live worker in a separate goroutine.
		go ri.liveWorker()
	}
	return ri
}

// getHostname returns the hostname of the machine.
func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// generate creates a Report based on the given parameters.
// For now, we skip detailed stack trace generation.
func (ri *ReportIssues) generate(issue string, extra map[string]interface{}, level string, options map[string]interface{}) *Report {
	// Compute a hash to throttle duplicate issues.
	hashInput := strings.ToLower(fmt.Sprintf("%s_issue_%s_%s", ri.AppName, level, issue))
	hash := crc32.ChecksumIEEE([]byte(hashInput))
	ri.LogDebug("Generated hash %d for issue '%s' (app: %s, level: %s)", hash, issue, ri.AppName, level)

	now := time.Now()
	ri.Mutex.Lock()
	nextAllowed, exists := ri.reported[hash]
	if exists && now.Before(nextAllowed) {
		ri.LogDebug("Issue '%s' for app '%s' reported too recently; skipping generation.", issue, ri.AppName)
		ri.Mutex.Unlock()
		return nil // Issue reported too recently.
	}
	// Set next allowed reporting time.
	ri.reported[hash] = now.Add(ri.Options.MinimumInterval)
	ri.Mutex.Unlock()

	// Caller information is not implemented; use placeholder.
	caller := "not_implemented"
	// Use unknown libVersion for now.
	libVersion := "unknown"

	report := Report{
		Version:     5,
		IssueID:     hash,
		Meta:        ri.Meta,
		Options:     options,
		Caller:      caller,
		StackTrace:  []string{}, // Not implemented.
		App:         ri.AppName,
		Extra:       extra,
		Description: issue,
		Level:       level,
		LibVersion:  libVersion,
		T:           now.UnixMilli(),
	}
	if ri.Options.Debug {
		ri.LogDebug("Report: %s", litter.Sdump(report))
	}
	return &report
}

// WaitQueue will wait for a maximum time or until the buffer is flushed.
func (ri *ReportIssues) WaitQueue(maxWait time.Duration) bool {
	ri.LogDebug("Waiting for queue to be flushed")
	timeout := time.After(maxWait)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			ri.LogDebug("waitQueue: Timeout reached, exiting wait.")
			return false
		case <-ticker.C:
			ri.Mutex.Lock()
			if len(ri.Buffer) == 0 {
				ri.Mutex.Unlock()
				ri.LogDebug("waitQueue: Buffer is empty, exiting wait.")
				return true
			}
			ri.Mutex.Unlock()
		}
	}
}

// Add creates and outputs a report.
// In live mode, the report is buffered; otherwise, it is written to a file.
func (ri *ReportIssues) Add(issue string, extra map[string]interface{}, level string, options map[string]interface{}) bool {
	report := ri.generate(issue, extra, level, options)

	if report == nil {
		return false
	}
	if ri.Options.Live {
		ri.Mutex.Lock()
		ri.Buffer = append(ri.Buffer, *report)
		ri.Mutex.Unlock()
		ri.LogDebug("Report added to live buffer: IssueID %d - total buffer size: %d", report.IssueID, len(ri.Buffer))
	} else {
		fileName := fmt.Sprintf("%d.coadmin_issue", report.IssueID)
		fullFilename := filepath.Join(ri.Options.Folder, fileName)
		data, err := json.Marshal(report)
		if err != nil {
			fmt.Printf("Error marshalling report: %v\n", err)
			return false
		}
		err = os.WriteFile(fullFilename, data, 0644)
		if err != nil {
			fmt.Printf("Error writing report file: %v\n", err)
			return false
		}
		ri.LogDebug("Report written to file: %s", fullFilename)
	}
	return true
}
func (ri *ReportIssues) liveWorker() {
	ri.LogDebug("Starting live worker")
	for {
		ri.Mutex.Lock()
		if len(ri.Buffer) > 0 {
			ri.LogDebug("Processing report from buffer")
			payload := ri.Buffer[0]
			ri.Buffer = ri.Buffer[1:]
			ri.Mutex.Unlock()
			ri.LogDebug("Sending HTTP POST request for IssueID %d", payload.IssueID)
			submission := ReportSubmission{
				Issue: payload,
			}
			resp, err := ri.restyClient.R().
				SetHeader("Content-Type", "application/json").
				SetBody(submission).
				Post(ri.Options.Server)
			if err != nil {
				fmt.Printf("Error sending HTTP request: %v\n", err)
			} else {
				ri.LogDebug("HTTP request sent, response status: %s", resp.Status())
			}
		} else {
			ri.Mutex.Unlock()
		}
		ri.LogDebug("Sleeping for 1 second, buffer size: %d", len(ri.Buffer))
		time.Sleep(1 * time.Second)
	}
}

// Convenience methods for different logging levels:

// Fatal reports an issue with "fatal" level.
func (ri *ReportIssues) Fatal(issue string, extra map[string]interface{}, options map[string]interface{}) bool {
	return ri.Add(issue, extra, "fatal", options)
}

// Warning reports an issue with "warning" level.
func (ri *ReportIssues) Warning(issue string, extra map[string]interface{}, options map[string]interface{}) bool {
	return ri.Add(issue, extra, "warning", options)
}

// Debug reports an issue with "debug" level.
func (ri *ReportIssues) Debug(issue string, extra map[string]interface{}, options map[string]interface{}) bool {
	return ri.Add(issue, extra, "debug", options)
}

// Info reports an issue with "info" level.
func (ri *ReportIssues) Info(issue string, extra map[string]interface{}, options map[string]interface{}) bool {
	return ri.Add(issue, extra, "info", options)
}

// Error reports an issue with "error" level.
func (ri *ReportIssues) Error(issue string, extra map[string]interface{}, options map[string]interface{}) bool {
	return ri.Add(issue, extra, "error", options)
}

// LogDebug prints debug messages if Debug mode is enabled.
func (ri *ReportIssues) LogDebug(format string, args ...interface{}) {
	if ri.Options.Debug {
		color.New(color.FgBlue).Printf("[DEBUG] "+format+"\n", args...)
	}
}
