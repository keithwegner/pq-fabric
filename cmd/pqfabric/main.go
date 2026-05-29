package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	apiauth "github.com/keithwegner/pq-fabric/core/auth"
	"github.com/keithwegner/pq-fabric/core/backup"
	"github.com/keithwegner/pq-fabric/core/consortium"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/observability"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "submit":
		return submit(args[1:])
	case "status":
		return status(args[1:])
	case "verify":
		return verify(args[1:])
	case "export-receipt":
		return exportReceipt(args[1:])
	case "anchor":
		return anchor(args[1:])
	case "report":
		return report(args[1:])
	case "auth":
		return authCommand(args[1:])
	case "manifest":
		return manifestCommand(args[1:])
	case "migrate-sqlite":
		return migrateSQLite(args[1:])
	case "backup":
		return backupSQLite(args[1:])
	case "restore-check":
		return restoreCheck(args[1:])
	case "help", "-h", "--help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func submit(args []string) error {
	fs := newFlagSet("submit")
	baseURL := commonURLFlag(fs)
	token := commonTokenFlag(fs)
	category := fs.String("category", "", "evidence category")
	artifactHash := fs.String("artifact-hash", "", "artifact hash")
	metadataHash := fs.String("metadata-hash", "", "metadata hash")
	org := fs.String("organization", "", "submitting organization")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key")
	anchorRequested := fs.Bool("anchor", false, "request optional testnet anchoring")
	if err := fs.Parse(args); err != nil {
		return err
	}
	submission := evidence.EvidenceSubmission{
		SchemaVersion:          evidence.SchemaVersion,
		EvidenceCategory:       *category,
		ArtifactHash:           *artifactHash,
		MetadataHash:           *metadataHash,
		SubmittingOrganization: *org,
		IdempotencyKey:         *idempotencyKey,
		AnchorRequested:        *anchorRequested,
	}
	return doJSON(http.MethodPost, *baseURL+"/v1/evidence", *token, submission, os.Stdout)
}

func status(args []string) error {
	fs := newFlagSet("status")
	baseURL := commonURLFlag(fs)
	token := commonTokenFlag(fs)
	evidenceID := fs.String("evidence-id", "", "evidence id")
	receiptID := fs.String("receipt-id", "", "receipt id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(*receiptID) != "":
		return doJSON(http.MethodGet, *baseURL+"/v1/receipts/"+*receiptID, *token, nil, os.Stdout)
	case strings.TrimSpace(*evidenceID) != "":
		return doJSON(http.MethodGet, *baseURL+"/v1/evidence/"+*evidenceID, *token, nil, os.Stdout)
	default:
		return errors.New("status requires --evidence-id or --receipt-id")
	}
}

func verify(args []string) error {
	fs := newFlagSet("verify")
	baseURL := commonURLFlag(fs)
	token := commonTokenFlag(fs)
	evidenceID := fs.String("evidence-id", "", "evidence id")
	receiptID := fs.String("receipt-id", "", "receipt id")
	receiptFile := fs.String("receipt-file", "", "receipt JSON file exported by pqfabric")
	if err := fs.Parse(args); err != nil {
		return err
	}
	req := evidence.VerificationRequest{ReceiptID: *receiptID, EvidenceID: *evidenceID}
	if strings.TrimSpace(*receiptFile) != "" {
		data, err := os.ReadFile(*receiptFile)
		if err != nil {
			return err
		}
		var receipt evidence.EvidenceReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return err
		}
		req = evidence.VerificationRequest{Receipt: &receipt}
	}
	return doJSON(http.MethodPost, *baseURL+"/v1/verify", *token, req, os.Stdout)
}

func exportReceipt(args []string) error {
	fs := newFlagSet("export-receipt")
	baseURL := commonURLFlag(fs)
	token := commonTokenFlag(fs)
	receiptID := fs.String("receipt-id", "", "receipt id")
	outPath := fs.String("out", "", "optional output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*receiptID) == "" {
		return errors.New("export-receipt requires --receipt-id")
	}
	var out bytes.Buffer
	if err := doJSON(http.MethodGet, *baseURL+"/v1/receipts/"+*receiptID, *token, nil, &out); err != nil {
		return err
	}
	if strings.TrimSpace(*outPath) == "" {
		_, err := io.Copy(os.Stdout, &out)
		return err
	}
	return os.WriteFile(*outPath, out.Bytes(), 0o644)
}

func anchor(args []string) error {
	fs := newFlagSet("anchor")
	baseURL := commonURLFlag(fs)
	token := commonTokenFlag(fs)
	qcHash := fs.String("qc-hash", "", "quorum certificate hash")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*qcHash) == "" {
		return errors.New("anchor requires --qc-hash")
	}
	return doJSON(http.MethodGet, *baseURL+"/v1/anchors/"+*qcHash, *token, nil, os.Stdout)
}

func report(args []string) error {
	fs := newFlagSet("report")
	baseURL := commonURLFlag(fs)
	token := commonTokenFlag(fs)
	format := fs.String("format", "json", "output format: json or text")
	outPath := fs.String("out", "", "optional output path")
	auditLimit := fs.Int("audit-limit", 50, "recent audit record limit")
	receiptLimit := fs.Int("receipt-limit", 20, "recent receipt/commit limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/v1/ops/report?audit_limit=%d&receipt_limit=%d", strings.TrimRight(*baseURL, "/"), *auditLimit, *receiptLimit)
	var raw bytes.Buffer
	if err := doJSON(http.MethodGet, url, *token, nil, &raw); err != nil {
		return err
	}
	var output []byte
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json", "":
		output = raw.Bytes()
	case "text":
		var report observability.OperatorReport
		if err := json.Unmarshal(raw.Bytes(), &report); err != nil {
			return err
		}
		output = []byte(observability.RenderReportText(report))
	default:
		return fmt.Errorf("unsupported report format %q", *format)
	}
	if strings.TrimSpace(*outPath) != "" {
		return os.WriteFile(*outPath, output, 0o644)
	}
	_, err := os.Stdout.Write(output)
	return err
}

func authCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pqfabric auth hash-token --token <token>")
	}
	switch args[0] {
	case "hash-token":
		return hashToken(args[1:])
	default:
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func manifestCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pqfabric manifest <generate|verify> [flags]")
	}
	switch args[0] {
	case "generate":
		return generateManifest(args[1:])
	case "verify":
		return verifyManifest(args[1:])
	default:
		return fmt.Errorf("unknown manifest command %q", args[0])
	}
}

func migrateSQLite(args []string) error {
	fs := newFlagSet("migrate-sqlite")
	databaseURL := fs.String("database-url", getenv("PQFABRIC_DATABASE_URL", ""), "SQLite database path or DSN")
	apply := fs.Bool("apply", false, "apply pending migrations; default is dry-run")
	format := fs.String("format", "json", "output format: json or text")
	outPath := fs.String("out", "", "optional output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*databaseURL) == "" {
		return errors.New("migrate-sqlite requires --database-url")
	}
	report, err := storage.CheckSQLiteMigrations(*databaseURL, !*apply)
	if err != nil {
		return err
	}
	return writeMigrationReport(report, *format, *outPath)
}

func backupSQLite(args []string) error {
	fs := newFlagSet("backup")
	databaseURL := fs.String("database-url", getenv("PQFABRIC_DATABASE_URL", ""), "source SQLite database path or DSN")
	backupDB := fs.String("backup-db", "", "destination backup SQLite database path")
	manifestPath := fs.String("manifest", "", "current consortium manifest for receipt verification")
	history := fs.String("history", "", "comma-separated manifest history for receipt verification")
	receiptLimit := fs.Int("receipt-limit", 20, "recent receipt verification limit")
	force := fs.Bool("force", false, "replace an existing backup database")
	format := fs.String("format", "json", "output format: json or text")
	outPath := fs.String("out", "", "optional report output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*databaseURL) == "" {
		return errors.New("backup requires --database-url")
	}
	if strings.TrimSpace(*backupDB) == "" {
		return errors.New("backup requires --backup-db")
	}
	report, err := backup.BackupSQLite(contextBackground(), backup.SQLiteOptions{
		SourceDatabase:  *databaseURL,
		BackupDatabase:  *backupDB,
		ManifestPath:    *manifestPath,
		ManifestHistory: *history,
		ReceiptLimit:    *receiptLimit,
		Force:           *force,
	})
	if writeErr := writeSQLiteReport(report, *format, *outPath); writeErr != nil {
		return writeErr
	}
	return err
}

func restoreCheck(args []string) error {
	fs := newFlagSet("restore-check")
	databaseURL := fs.String("database-url", "", "restored SQLite database path or DSN")
	manifestPath := fs.String("manifest", "", "current consortium manifest for receipt verification")
	history := fs.String("history", "", "comma-separated manifest history for receipt verification")
	receiptLimit := fs.Int("receipt-limit", 20, "recent receipt verification limit")
	format := fs.String("format", "json", "output format: json or text")
	outPath := fs.String("out", "", "optional report output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*databaseURL) == "" {
		return errors.New("restore-check requires --database-url")
	}
	report, err := backup.CheckSQLiteRestore(contextBackground(), backup.SQLiteOptions{
		BackupDatabase:  *databaseURL,
		ManifestPath:    *manifestPath,
		ManifestHistory: *history,
		ReceiptLimit:    *receiptLimit,
	})
	if writeErr := writeSQLiteReport(report, *format, *outPath); writeErr != nil {
		return writeErr
	}
	return err
}

func generateManifest(args []string) error {
	fs := newFlagSet("manifest generate")
	suiteName := fs.String("suite", getenv(cryptosuite.EnvVar, string(cryptosuite.Dev)), "crypto suite for generated fingerprints: dev or pq")
	consortiumID := fs.String("consortium-id", "local-consortium", "consortium id")
	membershipVersion := fs.Uint64("membership-version", 1, "positive membership version")
	threshold := fs.Int("threshold", 5, "quorum threshold")
	publicURLTemplate := fs.String("public-url-template", "http://{id}:8080", "public URL template; {id} is replaced with validator id")
	signingKeyRefTemplate := fs.String("signing-key-ref-template", "{key_id}", "signing key reference template; supports {id} and {key_id}")
	outPath := fs.String("out", "", "optional output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	selected, err := cryptosuite.Lookup(*suiteName)
	if err != nil {
		return err
	}
	urls := map[string]string{}
	for _, id := range identity.DefaultValidatorIDs() {
		urls[id] = strings.ReplaceAll(*publicURLTemplate, "{id}", id)
	}
	identities, err := identity.ValidatorIdentitiesForSuite(urls, selected)
	if err != nil {
		return err
	}
	manifest := consortium.ManifestFromIdentities(*consortiumID, *membershipVersion, *threshold, identities, identity.DefaultValidatorIDs())
	for i := range manifest.Validators {
		manifest.Validators[i].SigningKeyRef = strings.ReplaceAll(strings.ReplaceAll(*signingKeyRefTemplate, "{id}", manifest.Validators[i].ID), "{key_id}", manifest.Validators[i].KeyID)
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if strings.TrimSpace(*outPath) != "" {
		return os.WriteFile(*outPath, data, 0o644)
	}
	_, err = os.Stdout.Write(data)
	return err
}

func verifyManifest(args []string) error {
	fs := newFlagSet("manifest verify")
	manifestPath := fs.String("manifest", "", "current consortium manifest file")
	historyRaw := fs.String("history", "", "optional comma-separated historical consortium manifest files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*manifestPath) == "" {
		return errors.New("manifest verify requires --manifest")
	}
	manifest, err := consortium.LoadManifest(*manifestPath)
	if err != nil {
		return err
	}
	history := consortium.History{}
	if strings.TrimSpace(*historyRaw) != "" {
		history, err = consortium.LoadManifestHistory(*historyRaw)
		if err != nil {
			return err
		}
	}
	history, err = history.WithManifest(manifest)
	if err != nil {
		return err
	}
	hash, err := manifest.Hash()
	if err != nil {
		return err
	}
	summary := struct {
		Status            string   `json:"status"`
		ConsortiumID      string   `json:"consortium_id"`
		MembershipVersion uint64   `json:"membership_version"`
		ValidatorSetHash  string   `json:"validator_set_hash"`
		QuorumThreshold   int      `json:"quorum_threshold"`
		ActiveValidators  []string `json:"active_validators"`
		Operators         []string `json:"operators"`
		HistoryVersions   []uint64 `json:"history_versions"`
	}{
		Status:            "valid",
		ConsortiumID:      manifest.ConsortiumID,
		MembershipVersion: manifest.MembershipVersion,
		ValidatorSetHash:  hash,
		QuorumThreshold:   manifest.QuorumThreshold,
		ActiveValidators:  manifest.ActiveValidatorIDs(),
		Operators:         manifest.SortedOperators(),
		HistoryVersions:   manifestHistoryVersions(history),
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = os.Stdout.Write(data)
	return err
}

func manifestHistoryVersions(history consortium.History) []uint64 {
	out := make([]uint64, 0, len(history.Manifests))
	for _, manifest := range history.Manifests {
		out = append(out, manifest.MembershipVersion)
	}
	return out
}

func writeMigrationReport(report storage.SQLiteMigrationReport, format, outPath string) error {
	var output []byte
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json", "":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		output = append(data, '\n')
	case "text":
		output = []byte(fmt.Sprintf("pq-fabric sqlite migration report\nstatus=%s current=%d target=%d dry_run=%t pending=%v applied=%v\nmessage=%s\n",
			report.Status, report.CurrentVersion, report.TargetVersion, report.DryRun, report.PendingVersions, report.AppliedVersions, report.Message))
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	if strings.TrimSpace(outPath) != "" {
		return os.WriteFile(outPath, output, 0o644)
	}
	_, err := os.Stdout.Write(output)
	return err
}

func writeSQLiteReport(report backup.SQLiteReport, format, outPath string) error {
	var output []byte
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json", "":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		output = append(data, '\n')
	case "text":
		output = []byte(backup.Text(report))
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	if strings.TrimSpace(outPath) != "" {
		return os.WriteFile(outPath, output, 0o644)
	}
	_, err := os.Stdout.Write(output)
	return err
}

func hashToken(args []string) error {
	fs := newFlagSet("auth hash-token")
	token := fs.String("token", "", "API token to hash for server config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*token) == "" {
		return errors.New("auth hash-token requires --token; refusing to read or print secrets implicitly")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"token_hash": apiauth.HashToken(*token)})
}

func doJSON(method, url, token string, body any, out io.Writer) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	client := http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(method, strings.TrimRight(url, "/"), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s: %s", url, resp.Status, strings.TrimSpace(string(data)))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		pretty.WriteByte('\n')
		_, err = pretty.WriteTo(out)
		return err
	}
	_, err = out.Write(data)
	return err
}

func commonURLFlag(fs *flag.FlagSet) *string {
	return fs.String("url", getenv("PQFABRIC_URL", "http://127.0.0.1:8081"), "validator API base URL")
}

func commonTokenFlag(fs *flag.FlagSet) *string {
	return fs.String("token", getenv("PQFABRIC_API_TOKEN", ""), "API bearer token")
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func usage() error {
	fmt.Fprintln(os.Stderr, "usage: pqfabric <submit|status|verify|export-receipt|anchor|report|auth|manifest|migrate-sqlite|backup|restore-check> [flags]")
	return nil
}

func contextBackground() context.Context {
	return context.Background()
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
