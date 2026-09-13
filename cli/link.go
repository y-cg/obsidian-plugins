package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type linkOptions struct {
	plugins   string
	enabled   string
	vault     string
	configDir string
}

func newLinkCmd() *cobra.Command {
	opts := &linkOptions{}
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Install declared plugins into an Obsidian vault",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLink(opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.plugins, "plugins", "", "plugin JSON (id -> store dir or null)")
	f.StringVar(&opts.enabled, "enabled", "", "enabled ids JSON array")
	f.StringVar(&opts.vault, "vault", ".", "vault root")
	f.StringVar(&opts.configDir, "config-dir", ".obsidian", "config folder inside the vault")
	_ = cmd.MarkFlagRequired("plugins")
	_ = cmd.MarkFlagRequired("enabled")
	return cmd
}

func runLink(opts *linkOptions) error {
	if opts.plugins == "" || opts.enabled == "" {
		return errors.New("--plugins and --enabled are required")
	}

	plugins := map[string]*string{}
	if err := readJSON(opts.plugins, &plugins); err != nil {
		return fmt.Errorf("reading plugins: %w", err)
	}
	var enabled []string
	if err := readJSON(opts.enabled, &enabled); err != nil {
		return fmt.Errorf("reading enabled plugins: %w", err)
	}

	// Refuse to touch the vault while any declaration is unresolved: a vault
	// that lists plugins it does not have is worse than a loud failure.
	if missing := unresolved(plugins); len(missing) > 0 {
		return fmt.Errorf(
			"no resolved release for %s — the lock file is incomplete, run 'devenv tasks run obsidian:sync'",
			strings.Join(missing, ", "))
	}

	pluginsDir := filepath.Join(opts.vault, opts.configDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return err
	}
	if err := sweep(pluginsDir, plugins); err != nil {
		return fmt.Errorf("sweeping %s: %w", pluginsDir, err)
	}
	if err := install(pluginsDir, plugins); err != nil {
		return fmt.Errorf("installing plugins: %w", err)
	}

	community := filepath.Join(opts.vault, opts.configDir, "community-plugins.json")
	if err := replaceSymlink(opts.enabled, community); err != nil {
		return fmt.Errorf("linking community-plugins.json: %w", err)
	}
	return nil
}

// sweep removes store-owned symlinks that no longer belong to a declared
// plugin, then removes plugin directories that became empty. Files Obsidian
// writes itself (data.json and friends) are never touched.
func sweep(pluginsDir string, plugins map[string]*string) error {
	declared := make([]string, 0, len(plugins))
	for _, src := range plugins {
		if src != nil {
			declared = append(declared, *src)
		}
	}

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsDir, entry.Name())

		files, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.Type()&os.ModeSymlink == 0 {
				continue
			}
			path := filepath.Join(dir, f.Name())
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if strings.HasPrefix(target, "/nix/store/") && !belongsTo(target, declared) {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}

		remaining, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(remaining) == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

func belongsTo(target string, dirs []string) bool {
	for _, dir := range dirs {
		if strings.HasPrefix(target, dir+"/") {
			return true
		}
	}
	return false
}

// install puts each declared plugin into a real directory whose files are
// per-file symlinks into the store, so Obsidian can still create data.json
// next to them.
func install(pluginsDir string, plugins map[string]*string) error {
	for id, src := range plugins {
		if src == nil {
			// runLink rejects unresolved declarations before touching the vault,
			// so this guards future callers rather than a reachable path.
			return fmt.Errorf("%s has no lock entry", id)
		}

		dir := filepath.Join(pluginsDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		files, err := os.ReadDir(*src)
		if err != nil {
			return err
		}
		for _, f := range files {
			link := filepath.Join(dir, f.Name())
			target := filepath.Join(*src, f.Name())

			// Leave anything Obsidian or the user put there by hand alone.
			if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink == 0 {
				warnf("%s is a real file, not overwriting", link)
				continue
			}
			if err := replaceSymlink(target, link); err != nil {
				return err
			}
		}
	}
	return nil
}

// unresolved lists declared plugin ids whose release the lock does not pin.
func unresolved(plugins map[string]*string) []string {
	var missing []string
	for id, src := range plugins {
		if src == nil {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}

// replaceSymlink points path at target, leaving an already-correct symlink
// alone so repeated runs are true no-ops. Anything else — a stale symlink, or a
// real file Obsidian wrote — is replaced: the Nix configuration is the single
// source of truth, so external edits are undone on the next link.
func replaceSymlink(target, path string) error {
	if current, err := os.Readlink(path); err == nil && current == target {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, path)
}
