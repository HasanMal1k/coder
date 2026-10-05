package guest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

const (
	// teamFolder is where, under the home folder, the copy is kept.
	teamFolder = ".coder/team"
	// teamInterval is how often the control plane is asked whether anything
	// has changed. Asking is cheap: an unchanged record is answered with a
	// status and no body.
	teamInterval = 3 * time.Second
	// teamFollowUp is how soon it is asked again after services were
	// started or stopped. Asking is also how the machine says what it is
	// running, and an agent is waiting to hear that its service came up.
	teamFollowUp = 400 * time.Millisecond
	// maxTeamBody caps the record's size.
	maxTeamBody = 32 << 20
)

// errTeamUnavailable means the control plane has no record to give: it is
// an older one, from before machines kept a copy.
var errTeamUnavailable = errors.New("the control plane does not offer the team's record")

// RunTeamSync keeps a copy, on this machine's disk, of what the owner's
// machines and coding agents are doing and have done, until ctx is
// cancelled. The coding agents here read it to know about the others. It
// also keeps running the services the control plane says this machine has.
//
// The control plane decides what the files are, what they say and which
// services there are; this only fetches that and acts on it.
func RunTeamSync(ctx context.Context, cfg Config) {
	url, err := controlURL(cfg.ConnectURL, guestapi.TeamPath)
	if err != nil {
		log.Printf("team record: %v", err)
		return
	}

	sync := &teamSync{
		client:   &http.Client{Timeout: 30 * time.Second},
		url:      url,
		key:      cfg.InstanceID + "." + cfg.Token,
		dir:      filepath.Join(homeDir(), teamFolder),
		services: newServiceManager(ctx, filepath.Join(homeDir(), serviceLogFolder)),
	}

	wait := teamInterval
	failing := false

	for {
		changed, err := sync.once(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err == nil:
			if failing {
				log.Printf("team record: up to date again")
			}
			failing, wait = false, teamInterval
			if changed {
				wait = teamFollowUp
			}
		default:
			// Said once, not every few seconds for as long as it lasts.
			if !failing {
				log.Printf("team record: %v", err)
			}
			failing = true
			wait = min(wait*2, maxBackoff)
		}

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
}

type teamSync struct {
	client *http.Client
	url    string
	// key is this machine's identity, as it presents it to the control
	// plane.
	key string
	dir string
	// services runs what the record says this machine should be running.
	services *serviceManager
	// etag identifies the record last written, so the control plane can
	// say "no change" instead of sending it again.
	etag string
}

// once fetches the record and, if it has changed, writes it to disk and
// starts and stops services to match. In asking, it says which services this
// machine has started. It reports whether any service was started or
// stopped.
func (s *teamSync) once(ctx context.Context) (changed bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	if s.services != nil {
		if running, err := json.Marshal(s.services.started()); err == nil {
			req.Header.Set(guestapi.ServicesHeader, string(running))
		}
	}
	if s.etag != "" {
		req.Header.Set("If-None-Match", s.etag)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return false, nil
	case http.StatusOK:
	case http.StatusNotFound:
		return false, errTeamUnavailable
	default:
		return false, fmt.Errorf("the control plane answered %s", resp.Status)
	}

	var record struct {
		Files    map[string]string `json:"files"`
		Services []serviceDef      `json:"services"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTeamBody)).Decode(&record); err != nil {
		return false, fmt.Errorf("read the record: %w", err)
	}

	if err := writeFiles(s.dir, record.Files); err != nil {
		return false, err
	}
	if s.services != nil {
		changed = s.services.reconcile(record.Services)
	}
	s.etag = resp.Header.Get("ETag")

	return changed, nil
}

// writeFiles makes dir hold exactly the given files, by path relative to it.
// Files already there with the same content are left untouched; files no
// longer in the set are removed.
func writeFiles(dir string, files map[string]string) error {
	// Paths come from the control plane, which is trusted, but a path that
	// climbs out of the folder is refused all the same: this runs with the
	// user's own permissions, in their home folder.
	for path := range files {
		if !filepath.IsLocal(path) {
			return fmt.Errorf("the record contains a file outside its folder: %q", path)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	for path, content := range files {
		target := filepath.Join(dir, path)

		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, []byte(content)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create folder for %s: %w", path, err)
		}

		// Written beside the file and moved into place, so that an agent
		// reading it never sees half of it.
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := os.Rename(tmp, target); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	// Whatever else is in the folder was written by an earlier record: an
	// agent or a machine that no longer exists.
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if _, wanted := files[filepath.ToSlash(rel)]; !wanted {
			return os.Remove(path)
		}

		return nil
	})
}
