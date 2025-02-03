package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/7c/coadmin-golib/issues"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	app         string
	description string
	level       string
	live        bool
	server      string
	debug       bool
	wait        time.Duration
)

var validLevels = []string{"warning", "error", "info", "debug", "fatal"}
var logDebug = log.New(os.Stdout, color.New(color.FgCyan).Sprint("[DEBUG] "), 0)

func main() {
	rootCmd := &cobra.Command{
		Use:   "coadmin-cli",
		Short: "Coadmin CLI tool",
	}

	// 'issue' command
	issueCmd := &cobra.Command{
		Use:   "issue",
		Short: "Handle issues",
	}

	// 'submit' subcommand under 'issue'
	submitCmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a new issue",
		Run:   runSubmit,
	}

	// Setup flags for 'issue submit'
	submitCmd.Flags().StringVar(&app, "app", "", "Application name (min 3 characters)")
	submitCmd.Flags().StringVar(&description, "description", "", "Issue description (min 3 characters)")
	submitCmd.Flags().StringVar(&level, "level", "", "Issue level (warning|error|info|debug|fatal)")
	submitCmd.Flags().BoolVar(&live, "live", false, "Enable live mode")
	submitCmd.Flags().StringVar(&server, "server", "", "Server URL (required if live mode is enabled)")
	submitCmd.Flags().DurationVar(&wait, "wait", 10*time.Second, "Wait for the issue to be submitted (max 10 seconds)")
	submitCmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")

	// Mark required flags.
	submitCmd.MarkFlagRequired("app")
	submitCmd.MarkFlagRequired("description")
	submitCmd.MarkFlagRequired("level")

	issueCmd.AddCommand(submitCmd)
	rootCmd.AddCommand(issueCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runSubmit(cmd *cobra.Command, args []string) {
	var errMessages []string

	// Validate --app
	if len(app) < 3 {
		errMessages = append(errMessages, "--app must be at least 3 characters")
	}

	// Validate --description
	if len(description) < 3 {
		errMessages = append(errMessages, "--description must be at least 3 characters")
	}

	// Validate --level
	lowerLevel := strings.ToLower(level)
	if !contains(validLevels, lowerLevel) {
		errMessages = append(errMessages, fmt.Sprintf("--level must be one of: %s", strings.Join(validLevels, ", ")))
	}

	// Validate live mode options if --live is set
	if live {
		if server == "" {
			errMessages = append(errMessages, "--server is required in live mode")
		} else {
			if _, err := url.ParseRequestURI(server); err != nil {
				errMessages = append(errMessages, "--server must be a valid URL")
			}
		}

	}

	// If any validations failed, show all errors and exit.
	if len(errMessages) > 0 {
		fmt.Println("Error: Invalid arguments")
		for _, msg := range errMessages {
			fmt.Println("-", msg)
		}
		os.Exit(1)
	}

	// Display parameters for confirmation.
	fmt.Println("Submitting issue with parameters:")
	fmt.Printf("App: %s\n", app)
	fmt.Printf("Description: %s\n", description)
	fmt.Printf("Level: %s\n", lowerLevel)
	if live {
		fmt.Println("Live mode enabled")
		fmt.Printf("Server: %s\n", server)
	}

	// Initialize ReportIssues with appropriate options.
	opts := issues.Options{
		Live:            live,
		Server:          server,
		MinimumInterval: 60 * time.Second,
		Folder:          "/var/coadmin",
		Output:          false,
		Debug:           debug,
	}
	ri := issues.NewReportIssues(app, &opts)

	extra := make(map[string]interface{})
	repOptions := make(map[string]interface{})
	success := ri.Add(description, extra, lowerLevel, repOptions)

	// In live mode, allow time for liveWorker to process the buffered report.
	if live {
		logDebug.Printf("Waiting for liveWorker to process the buffered report, max %s", wait)
		submitted := ri.WaitQueue(wait)
		if !submitted {
			fmt.Println("Issue submission failed")
			os.Exit(1)
		} else {
			fmt.Println("Issue submitted successfully")
			os.Exit(0)
		}

	} else {
		if success {
			fmt.Println("Issue submitted successfully")
			os.Exit(0)
		} else {
			fmt.Println("Issue submission failed")
			os.Exit(1)
		}
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
