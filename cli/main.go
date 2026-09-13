// Command obsidian-plugins carries the logic for declaratively managing
// Obsidian community plugins from a devenv module.
//
// It reads and writes the JSON interchange files produced by the Nix module
// (declarations, lock, resolved plugin paths) so that the module can stay a
// pure data layer and this binary can own all the I/O and network work.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "obsidian-plugins",
		Short:         "Declaratively install and synchronize Obsidian community plugins",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newSyncCmd(), newLinkCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "obsidian-plugins: %v\n", err)
		os.Exit(1)
	}
}

// readJSON decodes path into v. A missing file is an error; an existing but
// empty file is treated as no value (devenv leaves empty lock files behind).
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// writeJSON writes v as indented JSON via a temp file + rename so a failed
// write never leaves a truncated lock behind.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "obsidian: "+format+"\n", args...)
}
