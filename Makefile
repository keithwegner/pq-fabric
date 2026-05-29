.PHONY: verify test demo fault-demo failure-evidence routing-demo routing-evidence routing-tests bundle-tests bundle-demo bundle-evidence contract-tests anchor-tests anchor-demo anchor-evidence validator pqfabric fmt lint-lite crypto-vectors crypto-vectors-mlkem crypto-vectors-mldsa image compose-config compose-up compose-down compose-logs compose-clean deploy-local deploy-local-smoke deployment-check k8s-validate terraform-validate deployment-evidence pilot-bootstrap-check pilot-backup-check pilot-deploy-check release-provenance release-provenance-check release-artifacts-check sqlite-restore-check e2e-demo e2e-evidence docs-check observability-check repo-hygiene final-verify tidy package package-handoff package-evidence

verify: test demo

fmt:
	gofmt -w ./cmd ./core ./consensus ./bundle ./routing ./contracts/client ./tests

lint-lite:
	go vet ./...

tidy:
	go mod tidy

test:
	go test ./...

demo:
	go run ./cmd/demo

fault-demo:
	rm -rf tmp/fault-demo-data tmp/failure-evidence.json tmp/failure-evidence.txt
	go run ./cmd/fault-demo

failure-evidence: fault-demo

routing-tests:
	go test ./routing/...

routing-demo:
	go run ./cmd/routing-demo

routing-evidence: routing-demo

bundle-tests:
	go test ./bundle/...

bundle-demo:
	go run ./cmd/bundle-demo

bundle-evidence: bundle-demo

contract-tests:
	@if command -v forge >/dev/null 2>&1; then cd contracts/polygon && forge test; else echo "forge not installed; skipping Foundry contract tests"; fi

anchor-tests:
	go test ./core/anchors/... ./contracts/client/...

anchor-demo:
	go run ./cmd/anchor-demo

anchor-evidence: anchor-demo

crypto-vectors: crypto-vectors-mlkem crypto-vectors-mldsa

crypto-vectors-mlkem:
	go test -count=1 -v ./core/crypto/mlkem -run TestMLKEMVectorFixtures

crypto-vectors-mldsa:
	go test -count=1 -v ./core/crypto/mldsa -run TestMLDSAVectorFixtures

validator:
	go run ./cmd/validator

pqfabric:
	go run ./cmd/pqfabric

image:
	@if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then docker build -t pq-fabric:local .; else echo "docker daemon unavailable; skipping image build"; fi

compose-config:
	@if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then docker compose -f docker-compose.yml config >/dev/null; else echo "docker compose not installed; skipping compose config validation"; fi

compose-up:
	docker compose -f docker-compose.yml --profile relays up -d

compose-down:
	docker compose -f docker-compose.yml --profile relays down

compose-logs:
	docker compose -f docker-compose.yml --profile relays logs --tail=100

compose-clean:
	docker compose -f docker-compose.yml --profile relays down --volumes --remove-orphans
	rm -rf ./data/validator-1 ./data/validator-2 ./data/validator-3 ./data/validator-4 ./data/validator-5 ./data/validator-6 ./data/validator-7

deploy-local: deployment-check compose-config deployment-evidence

deploy-local-smoke:
	./scripts/local-deployment-smoke.sh

deployment-check:
	./scripts/deployment-check.sh

k8s-validate:
	@if command -v kubectl >/dev/null 2>&1; then kubectl kustomize deployments/k8s/base >/dev/null; kubectl kustomize deployments/k8s/overlays/staging >/dev/null; kubectl kustomize deployments/k8s/overlays/production-pilot >/dev/null; else echo "kubectl not installed; skipping Kubernetes manifest validation"; fi

terraform-validate:
	@if command -v terraform >/dev/null 2>&1; then terraform -chdir=deployments/terraform init -backend=false && terraform -chdir=deployments/terraform validate; rm -rf deployments/terraform/.terraform; else echo "terraform not installed; skipping Terraform validation"; fi

deployment-evidence:
	go run ./cmd/deployment-evidence

pilot-bootstrap-check:
	mkdir -p tmp
	go run ./cmd/pilot-bootstrap validate --spec config/examples/pilot-bootstrap.example.yaml --format json --out tmp/pilot-bootstrap-validate.json
	go run ./cmd/pilot-bootstrap smoke --spec config/examples/pilot-bootstrap.example.yaml --format json --out tmp/pilot-bootstrap-smoke.json

pilot-backup-check:
	rm -rf tmp/pilot-backup-check
	mkdir -p tmp/pilot-backup-check
	go run ./cmd/pqfabric migrate-sqlite --database-url tmp/pilot-backup-check/source.db --apply --format json --out tmp/pilot-backup-migration.json
	go run ./cmd/pqfabric migrate-sqlite --database-url tmp/pilot-backup-check/source.db --format json --out tmp/pilot-backup-migration-dry-run.json
	go run ./cmd/pqfabric backup --database-url tmp/pilot-backup-check/source.db --backup-db tmp/pilot-backup-check/backup.db --force --format json --out tmp/pilot-backup-report.json
	go run ./cmd/pqfabric restore-check --database-url tmp/pilot-backup-check/backup.db --format json --out tmp/pilot-backup-restore-report.json

pilot-deploy-check:
	./scripts/pilot-deploy-check.sh

release-provenance:
	./scripts/release-provenance.sh

release-provenance-check:
	./scripts/release-provenance.sh >/tmp/pq-fabric-release-provenance-check.log
	rg -q '"schema_version": "pq-fabric.release-provenance.v1"' tmp/release-provenance.json
	rg -q '"status": "pass|pass_with_skips"' tmp/release-provenance.json
	rg -q '"go_module_inventory"' tmp/release-provenance.json
	rg -q '"image_reference"' tmp/release-provenance.json
	rg -q '"image_digest_file"' tmp/release-provenance.json
	rg -q '"sbom_status"' tmp/release-provenance.json
	rg -q '"cosign_status"' tmp/release-provenance.json
	rg -q '"cosign_verify_file"' tmp/release-provenance.json

release-artifacts-check:
	./scripts/release-artifacts-check.sh

sqlite-restore-check:
	go run ./cmd/sqlite-restore-check

e2e-demo:
	go run ./cmd/e2e-demo

e2e-evidence:
	go run ./cmd/e2e-evidence

docs-check:
	./scripts/docs-check.sh

observability-check:
	./scripts/observability-check.sh

repo-hygiene:
	./scripts/repo_hygiene.sh

final-verify:
	$(MAKE) fmt
	$(MAKE) test
	$(MAKE) lint-lite
	$(MAKE) verify
	$(MAKE) crypto-vectors
	$(MAKE) fault-demo
	$(MAKE) routing-tests
	$(MAKE) bundle-tests
	$(MAKE) anchor-tests
	$(MAKE) contract-tests
	$(MAKE) deployment-check
	$(MAKE) docs-check
	$(MAKE) observability-check
	$(MAKE) pilot-bootstrap-check
	$(MAKE) pilot-backup-check
	$(MAKE) release-provenance-check
	$(MAKE) release-artifacts-check
	$(MAKE) pilot-deploy-check
	$(MAKE) repo-hygiene
	$(MAKE) k8s-validate
	$(MAKE) terraform-validate

package-handoff:
	./scripts/package_handoff.sh

package-evidence:
	./scripts/package_evidence.sh

package:
	./scripts/package_handoff.sh
