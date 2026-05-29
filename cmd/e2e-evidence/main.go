package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/keithwegner/pq-fabric/internal/e2e"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	report, err := e2e.Run(ctx, e2e.Options{OutputDir: "tmp", WriteArtifacts: true})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(e2e.Text(report))
	fmt.Println("evidence_json=tmp/e2e-evidence.json")
	fmt.Println("evidence_text=tmp/e2e-evidence.txt")
}
