package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	app := &application{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	if err := app.run(os.Args[1:]); err != nil {
		// Intent: Preserve the primary command failure while making secondary
		// logging and lock-release failures visible to the operator. Source: standalone stack specification
		if app.store != nil && app.store.lockFile != nil {
			if logErr := app.store.logResult(logEntry{
				Command: app.command, Outcome: "failure", Error: failureCategory(err),
			}); logErr != nil {
				fmt.Fprintf(os.Stderr, "record command failure: %v\n", logErr)
			}
		}
		fmt.Fprintln(os.Stderr, err)
		if app.store != nil {
			if closeErr := app.store.close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "close stack store after failure: %v\n", closeErr)
			}
		}
		switch {
		case errors.Is(err, errConflict):
			os.Exit(3)
		case errors.Is(err, errUsage):
			os.Exit(2)
		default:
			os.Exit(1)
		}
	}
	if app.store != nil {
		if err := app.store.close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func failureCategory(err error) string {
	switch {
	case errors.Is(err, errConflict):
		return "coordination conflict"
	case errors.Is(err, errUsage):
		return "invalid input"
	default:
		return "storage or recovery failure"
	}
}
