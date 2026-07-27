// Command suiter is the Coding-Agent CLI for 飞书/钉钉/企微/腾讯文档.
package main

import (
	"fmt"
	"os"

	"github.com/SuperMarioYL/suiter/internal/cli"
)

func main() {
	if err := cli.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
