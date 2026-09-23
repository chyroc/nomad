package main

import (
	"context"
	"fmt"
	"os"

	"github.com/chyroc/nomad/internal/cli"
)

func main() {
	opts, err := cli.ParseOptions(os.Args[1:])
	if err != nil {
		fail(err)
	}
	if opts.Help {
		fmt.Print(cli.HelpText(cli.Version))
		return
	}
	if opts.Version {
		fmt.Println("nomad", cli.Version)
		return
	}

	app := cli.New(opts, os.Stdin, os.Stdout)
	if err := app.Run(context.Background()); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "nomad:", err)
	os.Exit(1)
}
