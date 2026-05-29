package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/keithwegner/pq-fabric/core/deployment/bootstrap"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pilot-bootstrap validate|smoke --spec <path> [--format text|json] [--out <path>]")
	}
	command := args[0]
	switch command {
	case "validate":
		return runReport(args[1:], false)
	case "smoke":
		return runReport(args[1:], true)
	case "-h", "--help", "help":
		fmt.Println("usage: pilot-bootstrap validate|smoke --spec <path> [--format text|json] [--out <path>]")
		return nil
	default:
		return fmt.Errorf("unknown pilot-bootstrap command %q", command)
	}
}

func runReport(args []string, smoke bool) error {
	fs := flag.NewFlagSet("pilot-bootstrap", flag.ContinueOnError)
	specPath := fs.String("spec", "", "bootstrap spec YAML path")
	format := fs.String("format", "text", "output format: text or json")
	outPath := fs.String("out", "", "optional output path")
	timeout := fs.Duration("timeout", 45*time.Second, "validation timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*specPath) == "" {
		return errors.New("--spec is required")
	}
	spec, err := bootstrap.LoadSpec(*specPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	var report bootstrap.BootstrapReport
	if smoke {
		report, err = bootstrap.Smoke(ctx, spec)
		if err != nil {
			return err
		}
	} else {
		report = bootstrap.Validate(ctx, spec)
	}
	var buf bytes.Buffer
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		if err := report.MarshalJSONIndented(&buf); err != nil {
			return err
		}
	case "text", "":
		buf.WriteString(report.MarshalText())
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
	if strings.TrimSpace(*outPath) != "" {
		if err := os.WriteFile(*outPath, buf.Bytes(), 0o644); err != nil {
			return err
		}
	} else {
		_, _ = os.Stdout.Write(buf.Bytes())
	}
	if !report.OK() {
		return errors.New("pilot bootstrap check failed")
	}
	return nil
}
