package main

import (
	"context"
	"fmt"
	"os"

	"phi/internal/cli"
)

func main() {
	if err := cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "phi: %v\n", err)
		os.Exit(1)
	}
}
