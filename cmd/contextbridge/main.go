package main

import (
	"os"

	"github.com/ukyovfx/Context-Bridge/internal/cli"
)

var version = "1.2.0-rc.1"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
