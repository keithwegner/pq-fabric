package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/keithwegner/pq-fabric/core/deployment"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("pilot-deploy-check", flag.ContinueOnError)
	profile := fs.String("profile", deployment.ProfileLocal, "deployment profile: local, staging, or production-pilot")
	envFile := fs.String("env-file", "", "optional env-style profile file")
	format := fs.String("format", "text", "output format: text or json")
	outPath := fs.String("out", "", "optional output path")
	allowPlaceholders := fs.Bool("allow-placeholders", false, "allow externally mounted secret/config references when validating examples")
	if err := fs.Parse(args); err != nil {
		return err
	}
	values := map[string]string{}
	if strings.TrimSpace(*envFile) != "" {
		loaded, err := deployment.ParseEnvFile(*envFile)
		if err != nil {
			return err
		}
		values = deployment.MergeValues(values, loaded)
	}
	report := deployment.ValidateProfile(deployment.ValidationInput{
		Profile:           *profile,
		Values:            values,
		AllowPlaceholders: *allowPlaceholders,
	})
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
		return errors.New("pilot deployment profile check failed")
	}
	return nil
}
