package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type commandStatus struct {
	Available bool   `json:"available"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
}

type evidence struct {
	GeneratedAtUnixMilli          int64                    `json:"generated_at_unix_milli"`
	DockerfilePresent             bool                     `json:"dockerfile_present"`
	DockerImageBuildStatus        string                   `json:"docker_image_build_status"`
	DockerVersion                 commandStatus            `json:"docker_version"`
	DockerComposeVersion          commandStatus            `json:"docker_compose_version"`
	DockerComposeConfigValidation commandStatus            `json:"docker_compose_config_validation"`
	ValidatorServicesModeled      int                      `json:"validator_services_modeled"`
	RelayServicesModeled          int                      `json:"relay_services_modeled"`
	PersistentDataDirectories     []string                 `json:"persistent_data_directories"`
	LocalOnlyNetworking           bool                     `json:"local_only_networking"`
	KubernetesManifestValidation  commandStatus            `json:"kubernetes_manifest_validation"`
	KubernetesStagingValidation   commandStatus            `json:"kubernetes_staging_validation"`
	KubernetesPilotValidation     commandStatus            `json:"kubernetes_production_pilot_validation"`
	TerraformValidation           commandStatus            `json:"terraform_validation"`
	LocalProfileValidation        commandStatus            `json:"local_profile_validation"`
	ProductionPilotValidation     commandStatus            `json:"production_pilot_profile_validation"`
	PilotBootstrapValidation      commandStatus            `json:"pilot_bootstrap_validation"`
	PilotBootstrapSmoke           commandStatus            `json:"pilot_bootstrap_smoke"`
	PilotBackupMigration          commandStatus            `json:"pilot_backup_migration"`
	PilotBackupCheck              commandStatus            `json:"pilot_backup_check"`
	PilotBackupRestoreCheck       commandStatus            `json:"pilot_backup_restore_check"`
	SQLiteRestoreCheck            commandStatus            `json:"sqlite_restore_check"`
	ReleaseProvenance             commandStatus            `json:"release_provenance"`
	ReleaseProvenanceCheck        commandStatus            `json:"release_provenance_check"`
	ConfigTemplates               []string                 `json:"config_templates"`
	SecretHandlingGuardrails      []string                 `json:"secret_handling_guardrails"`
	DeploymentProfiles            []string                 `json:"deployment_profiles"`
	MakeTargets                   []string                 `json:"make_targets"`
	OptionalTools                 map[string]commandStatus `json:"optional_tools"`
	LimitationsStatement          string                   `json:"limitations_statement"`
}

func main() {
	report := evidence{
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		DockerfilePresent:    exists("Dockerfile"),
		OptionalTools:        map[string]commandStatus{},
		LimitationsStatement: "Controlled deployment readiness is a provider-neutral pilot scaffold only. It does not deploy cloud resources, does not run terraform apply, does not commit secrets, does not enable public routing or mainnet anchoring, and does not claim certification, production anonymity, or audited smart-contract security.",
	}

	report.DockerVersion = runOptional("docker", "--version")
	report.DockerComposeVersion = runOptional("docker", "compose", "version")
	report.DockerComposeConfigValidation = runOptional("docker", "compose", "-f", "docker-compose.yml", "config")
	report.DockerImageBuildStatus = dockerImageStatus()
	report.KubernetesManifestValidation = runOptional("kubectl", "kustomize", "deployments/k8s/base")
	report.KubernetesStagingValidation = runOptional("kubectl", "kustomize", "deployments/k8s/overlays/staging")
	report.KubernetesPilotValidation = runOptional("kubectl", "kustomize", "deployments/k8s/overlays/production-pilot")
	report.TerraformValidation = terraformValidate()
	report.LocalProfileValidation = runOptional("go", "run", "./cmd/pilot-deploy-check", "--profile", "local", "--env-file", "config/examples/local-dev.example.env", "--format", "json")
	report.ProductionPilotValidation = runOptional("go", "run", "./cmd/pilot-deploy-check", "--profile", "production-pilot", "--env-file", "config/examples/production-pilot.example.env", "--allow-placeholders", "--format", "json")
	report.PilotBootstrapValidation = runOptional("go", "run", "./cmd/pilot-bootstrap", "validate", "--spec", "config/examples/pilot-bootstrap.example.yaml", "--format", "json")
	report.PilotBootstrapSmoke = runOptional("go", "run", "./cmd/pilot-bootstrap", "smoke", "--spec", "config/examples/pilot-bootstrap.example.yaml", "--format", "json")
	_ = os.RemoveAll("tmp/deployment-evidence-backup")
	_ = os.MkdirAll("tmp/deployment-evidence-backup", 0o755)
	report.PilotBackupMigration = runOptional("go", "run", "./cmd/pqfabric", "migrate-sqlite", "--database-url", "tmp/deployment-evidence-backup/source.db", "--apply", "--format", "json")
	report.PilotBackupCheck = runOptional("go", "run", "./cmd/pqfabric", "backup", "--database-url", "tmp/deployment-evidence-backup/source.db", "--backup-db", "tmp/deployment-evidence-backup/backup.db", "--force", "--format", "json")
	report.PilotBackupRestoreCheck = runOptional("go", "run", "./cmd/pqfabric", "restore-check", "--database-url", "tmp/deployment-evidence-backup/backup.db", "--format", "json")
	report.SQLiteRestoreCheck = runOptional("go", "run", "./cmd/sqlite-restore-check")
	report.ReleaseProvenance = runOptional("./scripts/release-provenance.sh")
	report.ReleaseProvenanceCheck = runOptional("make", "release-provenance-check")
	report.OptionalTools["docker"] = report.DockerVersion
	report.OptionalTools["docker_compose"] = report.DockerComposeVersion
	report.OptionalTools["kubectl"] = runOptional("kubectl", "version", "--client")
	report.OptionalTools["terraform"] = runOptional("terraform", "version")
	report.OptionalTools["forge"] = runOptional("forge", "--version")
	report.OptionalTools["syft"] = runOptional("syft", "version")
	report.OptionalTools["cosign"] = runOptional("cosign", "version")

	composeBytes, _ := os.ReadFile("docker-compose.yml")
	composeText := string(composeBytes)
	report.ValidatorServicesModeled = countServices(composeText, `validator-[1-7]`)
	report.RelayServicesModeled = countServices(composeText, `relay-[1-7]`)
	report.PersistentDataDirectories = persistentDirs(composeText)
	report.LocalOnlyNetworking = strings.Contains(composeText, "internal: true") &&
		strings.Contains(composeText, "127.0.0.1:8081:8080")
	report.ConfigTemplates = listFiles("config")
	report.SecretHandlingGuardrails = secretGuardrails()
	report.DeploymentProfiles = []string{"local", "staging", "production-pilot"}
	report.MakeTargets = []string{
		"image",
		"compose-config",
		"compose-up",
		"compose-down",
		"compose-logs",
		"compose-clean",
		"deploy-local",
		"deployment-check",
		"k8s-validate",
		"terraform-validate",
		"deployment-evidence",
		"pilot-bootstrap-check",
		"pilot-backup-check",
		"pilot-deploy-check",
		"sqlite-restore-check",
		"release-provenance",
		"release-provenance-check",
		"deploy-local-smoke",
		"package-handoff",
	}

	if err := os.MkdirAll("tmp", 0o755); err != nil {
		exitErr(err)
	}
	if err := writeJSON("tmp/deployment-evidence.json", report); err != nil {
		exitErr(err)
	}
	if err := os.WriteFile("tmp/deployment-evidence.txt", []byte(renderText(report)), 0o644); err != nil {
		exitErr(err)
	}

	fmt.Println("pq-fabric deployment evidence")
	fmt.Printf("dockerfile_present=%t image_status=%s\n", report.DockerfilePresent, report.DockerImageBuildStatus)
	fmt.Printf("compose validators=%d relays=%d local_only=%t validation=%s\n", report.ValidatorServicesModeled, report.RelayServicesModeled, report.LocalOnlyNetworking, report.DockerComposeConfigValidation.Status)
	fmt.Printf("k8s_validation=%s terraform_validation=%s\n", report.KubernetesManifestValidation.Status, report.TerraformValidation.Status)
	fmt.Printf("profiles local=%s production_pilot=%s bootstrap=%s smoke=%s backup=%s restore=%s sqlite_restore=%s release_provenance=%s release_check=%s\n", report.LocalProfileValidation.Status, report.ProductionPilotValidation.Status, report.PilotBootstrapValidation.Status, report.PilotBootstrapSmoke.Status, report.PilotBackupCheck.Status, report.PilotBackupRestoreCheck.Status, report.SQLiteRestoreCheck.Status, report.ReleaseProvenance.Status, report.ReleaseProvenanceCheck.Status)
	fmt.Printf("config_templates=%d guardrails=%d\n", len(report.ConfigTemplates), len(report.SecretHandlingGuardrails))
	fmt.Println("evidence_json=tmp/deployment-evidence.json")
	fmt.Println("evidence_text=tmp/deployment-evidence.txt")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runOptional(name string, args ...string) commandStatus {
	if _, err := exec.LookPath(name); err != nil {
		return commandStatus{Available: false, Status: "skipped: " + name + " not installed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimSpace(out.String())
	if len(output) > 500 {
		output = output[:500] + "...truncated"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return commandStatus{Available: true, Status: "failed: timed out", Output: output}
	}
	if err != nil {
		return commandStatus{Available: true, Status: "failed: " + err.Error(), Output: output}
	}
	return commandStatus{Available: true, Status: "pass", Output: output}
}

func dockerImageStatus() string {
	status := runOptional("docker", "image", "inspect", "pq-fabric:local")
	if !status.Available {
		return status.Status
	}
	if status.Status == "pass" {
		return "present: pq-fabric:local"
	}
	return "not present or not built in this run"
}

func terraformValidate() commandStatus {
	if _, err := exec.LookPath("terraform"); err != nil {
		return commandStatus{Available: false, Status: "skipped: terraform not installed"}
	}
	defer os.RemoveAll(filepath.Join("deployments", "terraform", ".terraform"))
	init := runOptional("terraform", "-chdir=deployments/terraform", "init", "-backend=false")
	if init.Status != "pass" {
		init.Status = "failed during init: " + init.Status
		return init
	}
	return runOptional("terraform", "-chdir=deployments/terraform", "validate")
}

func countServices(composeText, servicePattern string) int {
	re := regexp.MustCompile(`(?m)^  (` + servicePattern + `):\s*$`)
	matches := re.FindAllStringSubmatch(composeText, -1)
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		seen[match[1]] = struct{}{}
	}
	return len(seen)
}

func persistentDirs(composeText string) []string {
	re := regexp.MustCompile(`\./data/validator-[1-7]`)
	matches := re.FindAllString(composeText, -1)
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		seen[match] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for dir := range seen {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

func listFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		out = append(out, filepath.ToSlash(path))
		return nil
	})
	sort.Strings(out)
	return out
}

func secretGuardrails() []string {
	gitignore, _ := os.ReadFile(".gitignore")
	text := string(gitignore)
	checks := []string{
		".env",
		".env.*",
		"!*.example.env",
		"data/",
		"tmp/",
		"terraform.tfstate",
		"terraform.tfstate.*",
		".terraform/",
		"*.tfvars",
		"*.auto.tfvars",
		"*.pem",
		"*.key",
		"*.kubeconfig",
		"*wallet*",
		"contracts/polygon/broadcast/",
	}
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		status := "missing"
		if strings.Contains(text, check) {
			status = "present"
		}
		out = append(out, check+"="+status)
	}
	return out
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func renderText(report evidence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric deployment evidence\n")
	fmt.Fprintf(&b, "generated_at_unix_milli: %d\n", report.GeneratedAtUnixMilli)
	fmt.Fprintf(&b, "dockerfile_present: %t\n", report.DockerfilePresent)
	fmt.Fprintf(&b, "docker_image_build_status: %s\n", report.DockerImageBuildStatus)
	fmt.Fprintf(&b, "docker_compose_config_validation: %s\n", report.DockerComposeConfigValidation.Status)
	fmt.Fprintf(&b, "validator_services_modeled: %d\n", report.ValidatorServicesModeled)
	fmt.Fprintf(&b, "relay_services_modeled: %d\n", report.RelayServicesModeled)
	fmt.Fprintf(&b, "persistent_data_directories: %s\n", strings.Join(report.PersistentDataDirectories, ", "))
	fmt.Fprintf(&b, "local_only_networking: %t\n", report.LocalOnlyNetworking)
	fmt.Fprintf(&b, "kubernetes_manifest_validation: %s\n", report.KubernetesManifestValidation.Status)
	fmt.Fprintf(&b, "kubernetes_staging_validation: %s\n", report.KubernetesStagingValidation.Status)
	fmt.Fprintf(&b, "kubernetes_production_pilot_validation: %s\n", report.KubernetesPilotValidation.Status)
	fmt.Fprintf(&b, "terraform_validation: %s\n", report.TerraformValidation.Status)
	fmt.Fprintf(&b, "local_profile_validation: %s\n", report.LocalProfileValidation.Status)
	fmt.Fprintf(&b, "production_pilot_profile_validation: %s\n", report.ProductionPilotValidation.Status)
	fmt.Fprintf(&b, "pilot_bootstrap_validation: %s\n", report.PilotBootstrapValidation.Status)
	fmt.Fprintf(&b, "pilot_bootstrap_smoke: %s\n", report.PilotBootstrapSmoke.Status)
	fmt.Fprintf(&b, "pilot_backup_migration: %s\n", report.PilotBackupMigration.Status)
	fmt.Fprintf(&b, "pilot_backup_check: %s\n", report.PilotBackupCheck.Status)
	fmt.Fprintf(&b, "pilot_backup_restore_check: %s\n", report.PilotBackupRestoreCheck.Status)
	fmt.Fprintf(&b, "sqlite_restore_check: %s\n", report.SQLiteRestoreCheck.Status)
	fmt.Fprintf(&b, "release_provenance: %s\n", report.ReleaseProvenance.Status)
	fmt.Fprintf(&b, "release_provenance_check: %s\n", report.ReleaseProvenanceCheck.Status)
	fmt.Fprintf(&b, "config_templates: %s\n", strings.Join(report.ConfigTemplates, ", "))
	fmt.Fprintf(&b, "secret_handling_guardrails: %s\n", strings.Join(report.SecretHandlingGuardrails, ", "))
	fmt.Fprintf(&b, "deployment_profiles: %s\n", strings.Join(report.DeploymentProfiles, ", "))
	fmt.Fprintf(&b, "limitations: %s\n", report.LimitationsStatement)
	return b.String()
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
