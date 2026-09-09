package main

import (
	"os"

	"github.com/ukyovfx/Context-Bridge/internal/cli"
)

var version = "0.1.0-dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
