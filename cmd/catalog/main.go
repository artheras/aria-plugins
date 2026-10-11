package main

import (
	"fmt"
	"os"

	"github.com/artheras/aria-plugins/packages/catalog"
)

func main() {
	command := "check"
	if len(os.Args) == 2 {
		command = os.Args[1]
	} else if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/catalog [check|sync]")
		os.Exit(2)
	}
	var err error
	switch command {
	case "sync":
		err = catalog.Sync(".")
		if err == nil {
			err = catalog.Check(".")
		}
	case "check":
		err = catalog.Check(".")
	default:
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/catalog [check|sync]")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "catalogue:", err)
		os.Exit(1)
	}
	fmt.Println("Aria plugin catalogue, portable skills and integrity locks verified")
}
