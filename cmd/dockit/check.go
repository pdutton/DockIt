package main

import (
	"fmt"

	"github.com/pdutton/DockIt/internal/index"
	"github.com/pdutton/DockIt/internal/store"
)

func runCheck(e *env, args []string) error {
	flags := newFlagSet(e, "check", "<dir>")
	quiet := flags.Bool("q", false, "print errors only, not warnings or the summary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := dataDir(e, flags)
	if err != nil {
		return err
	}

	// Checking a dataset in use is allowed, since DockIt writes every file
	// atomically, but say so: the result is only as of this moment.
	if info, raw, _ := store.ReadLock(dir); (info != nil || raw != nil) && !*quiet {
		fmt.Fprintln(e.stdout, "Note: the dataset is locked, so an instance may be running:")
		printLock(e.stdout, info, raw)
	}

	x, report := index.Load(dir)
	for _, p := range report.Problems {
		if !p.Warning || !*quiet {
			fmt.Fprintln(e.stdout, p)
		}
	}
	if !*quiet {
		if x != nil {
			fmt.Fprintf(e.stdout, "Checked %s, %s and %s.\n",
				plural(len(x.Projects()), "project"), plural(x.TaskCount(), "task"), plural(len(x.Users()), "user"))
		}
		fmt.Fprintf(e.stdout, "%s, %s.\n",
			plural(report.Errors(), "error"), plural(report.Warnings(), "warning"))
	}
	if !report.OK() {
		return errSilent
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
