package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Soyuz-Tec/twintidy/internal/report"
	"github.com/Soyuz-Tec/twintidy/internal/scanner"
)

// Scan subcommand exit codes, chosen so scripts can branch on the result.
const (
	scanExitNoDuplicates = 0
	scanExitDuplicates   = 1
	scanExitError        = 2
)

// stringList collects a repeatable flag value.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ", ") }

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

// runScan executes the headless, strictly read-only scan subcommand. It
// reuses the production engine and report writer without initializing any
// GUI component, and never performs a cleanup action.
func runScan(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var (
		minSize    = flags.Int64("min-size", 0, "minimum file size in bytes; smaller files are ignored")
		formatName = flags.String("format", "json", "report format: csv or json")
		outPath    = flags.String("out", "", "write the report to this file atomically instead of stdout")
		quiet      = flags.Bool("quiet", false, "suppress the summary line on stderr")
		excludes   stringList
		excludeExt stringList
	)
	flags.Var(&excludes, "exclude", "exclude this path subtree (repeatable)")
	flags.Var(&excludeExt, "exclude-ext", "exclude this file extension, for example .tmp (repeatable)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: TwinTidy.exe scan [options] <folder> [<folder>...]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Find exact duplicate files without opening the desktop application.")
		fmt.Fprintln(stderr, "The scan is strictly read-only. Exit codes: 0 no duplicates,")
		fmt.Fprintln(stderr, "1 duplicates found, 2 error.")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return scanExitError
	}
	roots := flags.Args()
	if len(roots) == 0 {
		fmt.Fprintln(stderr, "scan requires at least one folder")
		flags.Usage()
		return scanExitError
	}

	format := report.Format(strings.ToLower(strings.TrimSpace(*formatName)))
	if format.Extension() == "" {
		fmt.Fprintf(stderr, "unsupported report format %q; use csv or json\n", *formatName)
		return scanExitError
	}

	options := scanner.DefaultScanOptions()
	options.MinFileSize = *minSize
	options.ExcludedPaths = excludes
	options.ExcludedExtensions = excludeExt

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	engine := scanner.NewEngine(0)
	groups, err := engine.ScanWithOptions(ctx, roots, options, nil)
	if err != nil {
		fmt.Fprintf(stderr, "scan failed: %v\n", err)
		return scanExitError
	}

	generatedAt := time.Now()
	folderLabel := strings.Join(roots, string(os.PathListSeparator))
	var summary report.Summary
	if *outPath != "" {
		_, err = report.WriteFileAtomic(ctx, *outPath, func(w io.Writer) error {
			summary, err = report.Write(ctx, w, format, folderLabel, groups, generatedAt)
			return err
		})
	} else {
		summary, err = report.Write(ctx, stdout, format, folderLabel, groups, generatedAt)
	}
	if err != nil {
		fmt.Fprintf(stderr, "report failed: %v\n", err)
		return scanExitError
	}

	if !*quiet {
		fmt.Fprintf(stderr, "%d duplicate group(s), %d file(s), reclaimable %d byte(s)\n",
			summary.GroupCount, summary.FileCount, summary.ReclaimableBytes)
	}
	if len(groups) > 0 {
		return scanExitDuplicates
	}
	return scanExitNoDuplicates
}
