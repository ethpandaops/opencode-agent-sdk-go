// Example: stat_session — read metadata for a single opencode session
// directly from its local SQLite store without starting an `opencode
// acp` subprocess.
//
//	go run ./examples/stat_session <session-id>
//
// Pick a session id from `Client.ListSessions` / `Client.IterSessions`
// or by inspecting opencode's own UI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/stat_session <session-id>")
		os.Exit(2)
	}

	sessionID := os.Args[1]

	stat, err := opencodesdk.StatSession(context.Background(), sessionID)
	if err != nil {
		if errors.Is(err, opencodesdk.ErrSessionNotFound) {
			fmt.Fprintf(os.Stderr, "session %s not found in opencode's local database\n", sessionID)
			os.Exit(2)
		}

		fmt.Fprintf(os.Stderr, "StatSession failed: %v\n", err)
		os.Exit(1)
	}

	b, _ := json.MarshalIndent(stat, "", "  ")
	fmt.Println(string(b))

	fmt.Printf("\n# humans\n")
	fmt.Printf("title        : %s\n", stat.Title)
	fmt.Printf("directory    : %s\n", stat.Directory)
	fmt.Printf("created      : %s (%s ago)\n", stat.CreatedAt.Format(time.RFC3339), time.Since(stat.CreatedAt).Truncate(time.Second))
	fmt.Printf("updated      : %s (%s ago)\n", stat.UpdatedAt.Format(time.RFC3339), time.Since(stat.UpdatedAt).Truncate(time.Second))
	fmt.Printf("messages     : %d\n", stat.MessageCount)
	fmt.Printf("archived     : %v\n", stat.Archived())
	fmt.Printf("opencode ver : %s\n", stat.Version)
}
