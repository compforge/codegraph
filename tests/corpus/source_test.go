package corpus_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type repository struct {
	Name          string `json:"name"`
	Repository    string `json:"repository"`
	Commit        string `json:"commit"`
	ArchiveSHA256 string `json:"archiveSHA256"`
	Language      string `json:"language,omitempty"`
	SourceRoot    string `json:"sourceRoot,omitempty"`
}

func repositories() ([]repository, error) {
	return readRepositories("repos.json")
}

func readRepositories(name string) ([]repository, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var repos []repository
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// Cache only checksum-verified archives; each run gets a fresh source copy.
func prepare(ctx context.Context, repo repository, dest string) error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	archive := filepath.Join(cache, "codegraph-corpus", repo.ArchiveSHA256+".tar.gz")
	data, err := os.ReadFile(archive)
	if os.IsNotExist(err) {
		url := "https://codeload.github.com/" + strings.TrimPrefix(repo.Repository, "https://github.com/") + "/tar.gz/" + repo.Commit
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("archive %s: %s", repo.Name, resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
		if err != nil {
			return err
		}
		if len(data) > 32<<20 {
			return fmt.Errorf("archive exceeds 32 MiB: %s", repo.Name)
		}
	} else if err != nil {
		return err
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != repo.ArchiveSHA256 {
		return fmt.Errorf("archive checksum mismatch: %s got %s", repo.Name, got)
	}
	if err := os.MkdirAll(filepath.Dir(archive), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(archive, data, 0644); err != nil {
		return err
	}
	return unpack(data, dest)
}

func unpack(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		_, relative, _ := strings.Cut(h.Name, "/")
		if relative == "" {
			continue
		}
		if !filepath.IsLocal(relative) {
			return fmt.Errorf("non-local archive path %q", h.Name)
		}
		name := filepath.Join(dest, relative)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(name, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if total > 128<<20 {
				return fmt.Errorf("expanded archive exceeds 128 MiB")
			}
			if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported archive entry %q type %d", h.Name, h.Typeflag)
		}
	}
}
