package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/keithwegner/pq-fabric/internal/e2e"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	report, err := e2e.Run(ctx, e2e.Options{OutputDir: "tmp", WriteArtifacts: false})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(e2e.Text(report))
	fmt.Println("integrated demo complete: local consensus, fault recovery, durability, routing, bundle, mock AI, mock anchors, and deployment validation were exercised without public infrastructure.")
}
