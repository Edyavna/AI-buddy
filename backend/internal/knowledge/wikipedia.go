package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

const wikipediaIDPrefix = "ru-wikipedia-avito-"
const wikipediaRevisionURL = "https://ru.wikipedia.org/w/index.php?title=%D0%90%D0%B2%D0%B8%D1%82%D0%BE&oldid="
const wikipediaLicenseURL = "https://creativecommons.org/licenses/by-sa/4.0/"
const originalDocumentURL = `COALESCE(pb.original_url,CASE WHEN d.source='` + ports.SourceWikipedia + `' THEN '` + wikipediaRevisionURL + `' || d.source_id ELSE '' END)`

type wikipediaManifest struct {
	Revision int64     `json:"revision"`
	Date     time.Time `json:"date"`
	URL      string    `json:"url"`
	Authors  string    `json:"authors"`
	License  string    `json:"license"`
	Changes  string    `json:"changes"`
	SHA256   string    `json:"sha256"`
}

// LoadWikipedia validates the offline snapshot of the Russian Wikipedia article.
func LoadWikipedia(dir string) (Document, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Document{}, fmt.Errorf("read Wikipedia manifest: %w", err)
	}
	var m wikipediaManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Document{}, fmt.Errorf("parse Wikipedia manifest: %w", err)
	}
	revision := strconv.FormatInt(m.Revision, 10)
	if m.Revision <= 0 || m.Date.IsZero() || m.URL != wikipediaRevisionURL+revision ||
		m.License != wikipediaLicenseURL || strings.TrimSpace(m.Authors) == "" || strings.TrimSpace(m.Changes) == "" {
		return Document{}, fmt.Errorf("invalid Wikipedia manifest: %w", ports.ErrInvalid)
	}
	data, err = os.ReadFile(filepath.Join(dir, "avito.md"))
	if err != nil {
		return Document{}, fmt.Errorf("read Wikipedia article: %w", err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 80_000 ||
		fmt.Sprintf("%x", sha256.Sum256([]byte(text))) != m.SHA256 {
		return Document{}, fmt.Errorf("invalid Wikipedia article or checksum: %w", ports.ErrInvalid)
	}
	attribution := fmt.Sprintf("Источник — [%s](%s), редакция от %s. Лицензия [CC BY-SA 4.0](%s). %s",
		m.Authors, m.URL, m.Date.UTC().Format(time.DateOnly), m.License, m.Changes)
	return Document{
		ID: wikipediaIDPrefix + revision, Source: ports.SourceWikipedia, SourceID: revision,
		Title: "Авито — Википедия", Content: text + "\n" + attribution, Version: m.Revision, UpdatedAt: m.Date,
	}, nil
}

// ImportWikipedia keeps each cited revision immutable and uses the normal index writer.
func (s *Service) ImportWikipedia(ctx context.Context, dir string) error {
	doc, err := LoadWikipedia(dir)
	if err != nil {
		return err
	}
	if err := s.importDocument(ctx, doc); err != nil {
		return fmt.Errorf("index Wikipedia revision %s: %w", doc.SourceID, err)
	}
	return nil
}
