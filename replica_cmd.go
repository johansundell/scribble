package main

import (
	"context"
	"fmt"
	"io"

	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

const replicaUsage = "usage: scribble-server replica resync"

// runReplica is the replica subcommand. "replica resync" makes the next
// sync push every note to FileMaker again, for a FileMaker file that was
// replaced or emptied (#66). The running service picks it up at its next
// round. It returns the process exit code.
func runReplica(args []string, s types.AppSettings, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "resync" {
		fmt.Fprintln(stderr, replicaUsage)
		return 2
	}
	if s.Storage != types.StorageSQLite {
		fmt.Fprintln(stderr, "the replica needs STORAGE=sqlite")
		return 1
	}
	db, err := store.NewSQLite(s.SqlitePath)
	if err != nil {
		fmt.Fprintln(stderr, "replica resync:", err)
		return 1
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ResyncReplica(ctx); err != nil {
		fmt.Fprintln(stderr, "replica resync:", err)
		return 1
	}
	fmt.Fprintln(stdout, "The next sync pushes every note to FileMaker again.")
	// A new file restarts the PendingEdits serial number, and rows at or
	// below the last applied ID are taken as already applied.
	if st, err := db.ReplicaState(ctx); err == nil && st.LastAppliedPendingID > 0 {
		fmt.Fprintf(stdout, "PendingEdits rows up to ID %d were applied already. In a new FileMaker file, set the ID field's next serial value above %d.\n",
			st.LastAppliedPendingID, st.LastAppliedPendingID)
	}
	return 0
}
