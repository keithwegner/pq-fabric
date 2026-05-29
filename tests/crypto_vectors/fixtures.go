// Package crypto_vectors loads compact ACVTS-style JSON fixtures used by the
// pq-fabric crypto adapter tests. These fixtures are engineering validation
// artifacts, not certification evidence.
package crypto_vectors

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type Metadata struct {
	Algorithm           string `json:"algorithm"`
	ParameterSet        string `json:"parameter_set"`
	FixtureSource       string `json:"fixture_source"`
	FixtureDate         string `json:"fixture_date"`
	ExpectedResultCount int    `json:"expected_result_count"`
	PassFailSummary     string `json:"pass_fail_summary"`
}

type Fixture[T any] struct {
	Metadata Metadata `json:"metadata"`
	Cases    []T      `json:"cases"`
}

func LoadFixture[T any](t testing.TB, pathParts ...string) Fixture[T] {
	t.Helper()
	path := filepath.Join(append([]string{baseDir(t)}, pathParts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vector fixture %s: %v", path, err)
	}
	var fixture Fixture[T]
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse vector fixture %s: %v", path, err)
	}
	if err := fixture.Metadata.Validate(len(fixture.Cases)); err != nil {
		t.Fatalf("invalid vector fixture metadata %s: %v", path, err)
	}
	return fixture
}

func (m Metadata) Validate(caseCount int) error {
	if m.Algorithm == "" {
		return fmt.Errorf("algorithm is required")
	}
	if m.ParameterSet == "" {
		return fmt.Errorf("parameter_set is required")
	}
	if m.FixtureSource == "" {
		return fmt.Errorf("fixture_source is required")
	}
	if m.FixtureDate == "" {
		return fmt.Errorf("fixture_date is required")
	}
	if m.ExpectedResultCount != caseCount {
		return fmt.Errorf("expected_result_count=%d does not match cases=%d", m.ExpectedResultCount, caseCount)
	}
	if m.PassFailSummary == "" {
		return fmt.Errorf("pass_fail_summary is required")
	}
	return nil
}

func DecodeHex(caseID, field, value string) ([]byte, error) {
	out, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("case %s field %s is not valid hex: %w", caseID, field, err)
	}
	return out, nil
}

func Report(t testing.TB, metadata Metadata, passed, failed int) {
	t.Helper()
	total := passed + failed
	t.Logf("%s %s vector validation: passed=%d failed=%d total=%d expected=%d summary=%s",
		metadata.Algorithm,
		metadata.ParameterSet,
		passed,
		failed,
		total,
		metadata.ExpectedResultCount,
		metadata.PassFailSummary,
	)
	if total != metadata.ExpectedResultCount {
		t.Fatalf("%s %s vector validation counted %d case(s), expected %d",
			metadata.Algorithm,
			metadata.ParameterSet,
			total,
			metadata.ExpectedResultCount,
		)
	}
	if failed > 0 {
		t.Fatalf("%s %s vector validation failed: passed=%d failed=%d total=%d",
			metadata.Algorithm,
			metadata.ParameterSet,
			passed,
			failed,
			total,
		)
	}
}

func baseDir(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate crypto vector fixture directory")
	}
	return filepath.Dir(file)
}
