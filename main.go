package main

import (
	"os"

	"github.com/gregtuc/memsync/cmd"
)

func main() {
	os.Exit(cmd.Execute(os.Args[1:]))
}
