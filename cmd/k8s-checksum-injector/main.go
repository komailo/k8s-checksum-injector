package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/komailo/k8s-checksum-injector/pkg/injector"
)

func main() {
	var modeStr string
	flag.StringVar(&modeStr, "mode", string(injector.ModeLabel), "inject checksums as 'label' or 'annotation'")
	flag.Parse()

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read stdin: %v\n", err)
		os.Exit(1)
	}

	output, err := injector.InjectChecksums(string(input), injector.Mode(modeStr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write([]byte(output)); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
		os.Exit(1)
	}
}
