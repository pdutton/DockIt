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
	var le *upgrade.LoadError
	if errors.As(err, &le) {
		for _, p := range le.Report.Problems {
			fmt.Fprintln(e.stdout, p)
		}
		fmt.Fprintf(e.stdout, "%s, %s.\n", plural(le.Report.Errors(), "error"), plural(le.Report.Warnings(), "warning"))
		fmt.Fprintln(e.stdout, "The dataset was not upgraded.  Fix the errors, then run `dockit upgrade` again.")
		return errSilent
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Upgraded the dataset from format %s to %s.\n", res.From, res.To)
	fmt.Fprintln(e.stdout, "Run `dockit check` to confirm the result.")
	return nil
}
