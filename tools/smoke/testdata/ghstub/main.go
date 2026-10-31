// Command ghstub exercises project setup only through the missing-login boundary.
// It never contacts a host or accepts credentials.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := strings.Join(os.Args[1:], " ")
	switch args {
	case "--version":
		fmt.Println("gh stub")
	case "issue create --help":
		fmt.Println("--parent --blocked-by")
	default:
		os.Exit(1)
	}
}
