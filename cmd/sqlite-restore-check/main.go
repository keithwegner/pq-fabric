package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/validator"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/storage"
)

type result struct {
	Status               string `json:"status"`
	GeneratedAtUnixMilli int64  `json:"generated_at_unix_milli"`
	ReceiptID            string `json:"receipt_id"`
	EvidenceID           string `json:"evidence_id"`
	OriginalDatabase     string `json:"original_database"`
	RestoredDatabase     string `json:"restored_database"`
	VerificationStatus   string `json:"verification_status"`
	Message              string `json:"message"`
}

func main() {
	report, err := run(context.Background())
	if err != nil {
		report.Status = "fail"
		report.Message = err.Error()
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(os.Stderr, string(data))
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(data))
}

func run(ctx context.Context) (result, error) {
	if os.Getenv(cryptosuite.EnvVar) == "" {
		if err := os.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev)); err != nil {
			return result{}, err
		}
	}
	tmp, err := os.MkdirTemp("", "pq-fabric-sqlite-restore-*")
	if err != nil {
		return result{}, err
	}
	defer os.RemoveAll(tmp)
	peerURLs := map[string]string{}
	peers := make([]*validator.Node, 0, 4)
	for _, id := range []string{"validator-2", "validator-3", "validator-4", "validator-5"} {
		addr, err := freeAddr()
		if err != nil {
			return result{}, err
		}
		url := "http://" + addr
		node, err := validator.NewNode(validator.Config{
			ID:              id,
			ListenAddr:      addr,
			PublicURL:       url,
			PeerURLs:        map[string]string{},
			Threshold:       5,
			RequestTimeout:  time.Second,
			ProposalTimeout: 20 * time.Millisecond,
			VoteTimeout:     time.Second,
		})
		if err != nil {
			return result{}, err
		}
		if err := node.Start(ctx); err != nil {
			_ = node.Close()
			return result{}, err
		}
		defer func(node *validator.Node) {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = node.Stop(shutdownCtx)
			_ = node.Close()
		}(node)
		peers = append(peers, node)
		peerURLs[id] = url
	}
	_ = peers

	primaryDB := filepath.Join(tmp, "primary", "validator.db")
	leader, err := validator.NewNode(validator.Config{
		ID:              "validator-1",
		ListenAddr:      "127.0.0.1:0",
		PublicURL:       "http://validator-1",
		PeerURLs:        peerURLs,
		Threshold:       5,
		RequestTimeout:  time.Second,
		ProposalTimeout: 50 * time.Millisecond,
		VoteTimeout:     time.Second,
		StorageMode:     storage.ModeSQLite,
		DatabaseURL:     primaryDB,
	})
	if err != nil {
		return result{}, err
	}
	submission := evidence.EvidenceSubmission{
		SchemaVersion:          evidence.SchemaVersion,
		EvidenceCategory:       "dr-restore-check",
		ArtifactHash:           "sha256:sqlite-restore-artifact",
		MetadataHash:           "sha256:sqlite-restore-metadata",
		SubmittingOrganization: "deployment-readiness",
		IdempotencyKey:         "sqlite-restore-check-" + fmt.Sprint(time.Now().UnixNano()),
	}
	receipt, err := leader.SubmitEvidence(ctx, submission)
	if err != nil {
		_ = leader.Close()
		return result{}, err
	}
	if err := leader.Close(); err != nil {
		return result{}, err
	}

	restoredDB := filepath.Join(tmp, "restored", "validator.db")
	if err := copySQLiteFiles(primaryDB, restoredDB); err != nil {
		return result{}, err
	}
	restored, err := validator.NewNode(validator.Config{
		ID:             "validator-1",
		ListenAddr:     "127.0.0.1:0",
		PublicURL:      "http://validator-1",
		PeerURLs:       map[string]string{},
		Threshold:      5,
		RequestTimeout: time.Second,
		StorageMode:    storage.ModeSQLite,
		DatabaseURL:    restoredDB,
	})
	if err != nil {
		return result{}, err
	}
	defer restored.Close()
	restoredReceipt, ok, err := restored.EvidenceReceiptByReceiptID(receipt.ReceiptID)
	if err != nil {
		return result{}, err
	}
	if !ok {
		return result{}, fmt.Errorf("restored SQLite database did not contain receipt %s", receipt.ReceiptID)
	}
	verification := restored.VerifyEvidenceReceipt(restoredReceipt)
	if !verification.Valid {
		return result{}, fmt.Errorf("restored receipt verification failed: %s", verification.Reason)
	}
	return result{
		Status:               "pass",
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		ReceiptID:            restoredReceipt.ReceiptID,
		EvidenceID:           restoredReceipt.EvidenceID,
		OriginalDatabase:     primaryDB,
		RestoredDatabase:     restoredDB,
		VerificationStatus:   verification.Status,
		Message:              "SQLite receipt survived local backup copy and restore verification",
	}, nil
}

func freeAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return addr, nil
}

func copySQLiteFiles(srcDB, dstDB string) error {
	if err := os.MkdirAll(filepath.Dir(dstDB), 0o755); err != nil {
		return err
	}
	matches, err := filepath.Glob(srcDB + "*")
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf("no SQLite files matched %s", srcDB)
	}
	for _, src := range matches {
		suffix := src[len(srcDB):]
		if err := copyFile(src, dstDB+suffix); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
