package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/upgrade"
)

func runUpgrade(e *env, args []string) error {
	flags := newFlagSet(e, "upgrade", "<dir>")
	backup := flags.String("backup", "",
		"directory to create the backup in (default: beside the dataset); must be outside the dataset")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := dataDir(e, flags)
	if err != nil {
		return err
	}
	res, err := upgrade.Run(dir, *backup, model.FormatCurrent, upgrade.Migrations, time.Now())
	if errors.Is(err, upgrade.ErrCurrent) {
		fmt.Fprintf(e.stdout, "The dataset is already in format %d; nothing to do.\n", model.FormatCurrent)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Upgraded the dataset from format %d to %d.\n", res.From, res.To)
	fmt.Fprintf(e.stdout, "The original is backed up in %s.\n", res.Backup)
	fmt.Fprintln(e.stdout, "Run `dockit check` to confirm the result, then remove the backup when you no longer need it.")
	return nil
}
