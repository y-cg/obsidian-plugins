package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/google/go-github/v91/github"
	"github.com/spf13/cobra"
)

const (
	defaultRegistryURL = "https://raw.githubusercontent.com/obsidianmd/obsidian-releases/HEAD/community-plugins.json"
	userAgent          = "obsidian-plugins"
	httpTimeout        = 60 * time.Second
)

// The release assets BRAT (and Obsidian itself) installs.
var wantedAssets = []string{"main.js", "manifest.json", "styles.css"}

// declaration is one entry of the Nix-generated declarations file. A nil Repo
// means "look it up in the community registry"; a nil Version means "follow the
// repo's latest release".
type declaration struct {
	Repo    *string `json:"repo"`
	Version *string `json:"version"`
}

// lockEntry is one entry of the lock file: a fully pinned release.
type lockEntry struct {
	Repo    string            `json:"repo"`
	Version string            `json:"version"`
	Assets  map[string]string `json:"assets"`
}

type registryEntry struct {
	ID   string `json:"id"`
	Repo string `json:"repo"`
}

type syncOptions struct {
	declarations string
	lock         string
	registry     string
	update       bool
}

func newSyncCmd() *cobra.Command {
	opts := &syncOptions{}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Resolve declared plugins to pinned releases and write the lock file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd.Context(), opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.declarations, "declarations", "", "declarations JSON (id -> {repo, version})")
	f.StringVar(&opts.lock, "lock", "", "lock file to write")
	f.StringVar(&opts.registry, "registry", "", "community-plugins.json path or URL; empty fetches the official registry")
	f.BoolVar(&opts.update, "update", false, "re-resolve plugins that are already locked")
	_ = cmd.MarkFlagRequired("declarations")
	_ = cmd.MarkFlagRequired("lock")
	return cmd
}

// syncer bundles the two HTTP clients: go-github for release metadata and
// resty for the raw asset/registry bytes.
type syncer struct {
	gh   *github.Client
	http *resty.Client
}

func newSyncer() (*syncer, error) {
	httpClient := &http.Client{Timeout: httpTimeout}
	opts := []github.ClientOptionsFunc{
		github.WithHTTPClient(httpClient),
		github.WithUserAgent(userAgent),
	}
	if token := githubToken(); token != "" {
		opts = append(opts, github.WithAuthToken(token))
	}
	gh, err := github.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &syncer{
		gh:   gh,
		http: resty.NewWithClient(httpClient).SetHeader("User-Agent", userAgent),
	}, nil
}

func runSync(ctx context.Context, opts *syncOptions) error {
	decls := map[string]declaration{}
	if err := readJSON(opts.declarations, &decls); err != nil {
		return fmt.Errorf("reading declarations: %w", err)
	}
	lock := map[string]lockEntry{}
	if err := readJSON(opts.lock, &lock); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading lock: %w", err)
	}

	s, err := newSyncer()
	if err != nil {
		return err
	}
	registry, err := loadRegistryIfNeeded(ctx, s.http, opts.registry, decls)
	if err != nil {
		return err
	}

	changed := false
	for id, d := range decls {
		if upToDate(lock[id], d, opts.update) {
			continue
		}
		if err := s.resolveInto(ctx, lock, registry, id, d, opts.update); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		changed = true
	}

	if !changed {
		warnf("lock is up to date")
		return nil
	}
	if err := writeJSON(opts.lock, lock); err != nil {
		return fmt.Errorf("writing lock: %w", err)
	}
	warnf("wrote %s", opts.lock)
	return nil
}

// upToDate reports whether an existing lock entry already satisfies the
// declaration. An unset field on the declaration matches anything; --update
// disables the check entirely.
func upToDate(entry lockEntry, d declaration, update bool) bool {
	if update || entry.Repo == "" {
		return false
	}
	if d.Repo != nil && entry.Repo != *d.Repo {
		return false
	}
	if d.Version != nil && entry.Version != *d.Version {
		return false
	}
	return true
}

// resolveInto pins one declaration into lock, resolving repo/version as needed.
func (s *syncer) resolveInto(ctx context.Context, lock map[string]lockEntry, registry map[string]string, id string, d declaration, update bool) error {
	repo := ""
	switch {
	case d.Repo != nil:
		repo = *d.Repo
	default:
		r, ok := registry[id]
		if !ok {
			return errors.New("not in the community registry — set `repo` explicitly")
		}
		repo = r
	}

	existing := lock[id]
	var version string
	switch {
	case d.Version != nil:
		version = *d.Version
	case existing.Repo == repo && !update:
		version = existing.Version
	default:
		v, err := s.latestTag(ctx, repo)
		if err != nil {
			return err
		}
		version = v
	}

	warnf("fetching %s from %s@%s", id, repo, version)
	assets, err := s.resolveAssets(ctx, repo, version, id)
	if err != nil {
		return err
	}
	lock[id] = lockEntry{Repo: repo, Version: version, Assets: assets}
	return nil
}

func (s *syncer) latestTag(ctx context.Context, repo string) (string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return "", err
	}
	release, _, err := s.gh.Repositories.GetLatestRelease(ctx, owner, name)
	if err != nil {
		return "", fmt.Errorf("latest release: %w", err)
	}
	if release.GetTagName() == "" {
		return "", errors.New("latest release has no tag")
	}
	return release.GetTagName(), nil
}

// resolveAssets fetches the release, verifies it belongs to id, and returns the
// SRI hash of every wanted asset it provides.
func (s *syncer) resolveAssets(ctx context.Context, repo, version, id string) (map[string]string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	release, _, err := s.gh.Repositories.GetReleaseByTag(ctx, owner, name, version)
	if err != nil {
		return nil, fmt.Errorf("release %s: %w", version, err)
	}

	urls := map[string]string{}
	for _, asset := range release.Assets {
		if isWanted(asset.GetName()) {
			urls[asset.GetName()] = asset.GetBrowserDownloadURL()
		}
	}
	for _, required := range []string{"main.js", "manifest.json"} {
		if _, ok := urls[required]; !ok {
			return nil, fmt.Errorf("release %s has no %s asset", version, required)
		}
	}

	assets := map[string]string{}
	for assetName, assetURL := range urls {
		body, err := fetchBytes(ctx, s.http, assetURL)
		if err != nil {
			return nil, fmt.Errorf("downloading %s: %w", assetName, err)
		}
		sum := sha256.Sum256(body)
		assets[assetName] = "sha256-" + base64.StdEncoding.EncodeToString(sum[:])

		if assetName == "manifest.json" {
			var manifest struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(body, &manifest); err != nil {
				return nil, fmt.Errorf("parsing manifest.json: %w", err)
			}
			if manifest.ID != id {
				return nil, fmt.Errorf("manifest.json declares id %q", manifest.ID)
			}
		}
	}
	return assets, nil
}

// loadRegistryIfNeeded returns an id -> repo map, fetching the registry only
// when at least one declaration left its repo unset.
func loadRegistryIfNeeded(ctx context.Context, http *resty.Client, source string, decls map[string]declaration) (map[string]string, error) {
	needed := false
	for _, d := range decls {
		if d.Repo == nil {
			needed = true
			break
		}
	}
	if !needed {
		return nil, nil
	}

	var data []byte
	var err error
	switch {
	case source == "":
		data, err = fetchBytes(ctx, http, defaultRegistryURL)
		if err != nil {
			return nil, fmt.Errorf("fetching community registry: %w", err)
		}
	case strings.HasPrefix(source, "http://"), strings.HasPrefix(source, "https://"):
		data, err = fetchBytes(ctx, http, source)
		if err != nil {
			return nil, fmt.Errorf("fetching registry %s: %w", source, err)
		}
	default:
		data, err = os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("reading registry %s: %w", source, err)
		}
	}

	var entries []registryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing registry: %w", err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.ID != "" && e.Repo != "" {
			out[e.ID] = e.Repo
		}
	}
	return out, nil
}

func fetchBytes(ctx context.Context, http *resty.Client, url string) ([]byte, error) {
	resp, err := http.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status())
	}
	return resp.Body(), nil
}

func isWanted(name string) bool {
	return slices.Contains(wantedAssets, name)
}

func splitRepo(repo string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return "", "", fmt.Errorf("invalid repo %q, want \"owner/name\"", repo)
	}
	return owner, name, nil
}

// githubToken lets users lift the unauthenticated API rate limit.
func githubToken() string {
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}
