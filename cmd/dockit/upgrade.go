package main

import (
	"errors"
	"fmt"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/upgrade"
)

func runUpgrade(e *env, args []string) error {
	flags := newFlagSet(e, "upgrade", "<dir>")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := dataDir(e, flags)
	if err != nil {
		return err
	}
	res, err := upgrade.Run(dir, model.FormatCurrent, upgrade.Migrations)
	if errors.Is(err, upgrade.ErrCurrent) {
		fmt.Fprintf(e.stdout, "The dataset is already in format %s; nothing to do.\n", model.FormatCurrent)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Upgraded the dataset from format %s to %s.\n", res.From, res.To)
	fmt.Fprintln(e.stdout, "Run `dockit check` to confirm the result.")
	return nil
}
