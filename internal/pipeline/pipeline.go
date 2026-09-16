// Package pipeline enchaîne les opérations sur la régie : recevoir avant le
// lancement d'OBS, publier après sa fermeture.
//
// Les deux moments ne sont pas choisis par confort. Ce sont les seuls où les
// fichiers d'OBS sur le disque reflètent la réalité : entre les deux, la vérité
// est en mémoire dans le processus OBS.
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gilforge/stream-team/internal/config"
	"github.com/gilforge/stream-team/internal/manifest"
	"github.com/gilforge/stream-team/internal/obs"
	"github.com/gilforge/stream-team/internal/storage"
)

// BackupsKept borne le nombre de collections locales conservées avant écrasement.
const BackupsKept = 10

type Engine struct {
	Dir       string // dossier de l'agent
	Cfg       *config.Config
	Pub       *config.Publisher
	Paths     *obs.Paths
	Reader    storage.Reader
	Overrides *config.Overrides
}

// Report décrit ce qu'une opération a réellement fait, pour l'afficher dans le
// dock et dans la fenêtre de progression.
type Report struct {
	UpToDate      bool
	Version       int
	Author        string
	Message       string
	AssetsFetched int
	AssetsSkipped int
	CanvasChanged bool
	Unconfigured  []string
	Published     bool
}

func (e *Engine) collectionPath() (string, error) {
	path, err := e.Paths.CollectionFile(e.Cfg.Collection)
	if err == nil {
		return path, nil
	}
	// Collection encore absente : premier lancement sur cette machine.
	return e.Paths.NewCollectionFile(e.Cfg.Collection), nil
}

func (e *Engine) assetPath(rel string) string {
	rel = strings.TrimPrefix(strings.TrimPrefix(rel, "assets/"), "/")
	return filepath.Join(e.Cfg.AssetsDir, filepath.FromSlash(rel))
}

// RemoteManifest lit le manifeste publié. Toujours sans cache : un manifeste
// périmé ferait croire à un membre qu'il est à jour.
func (e *Engine) RemoteManifest(ctx context.Context) (*manifest.Manifest, error) {
	data, err := e.Reader.GetNoCache(ctx, manifest.Name)
	if err != nil {
		return nil, err
	}
	return manifest.Parse(data)
}

// LocalHash renvoie l'empreinte de la collection locale sous sa forme
// normalisée, comparable à celle enregistrée lors de la dernière réception.
func (e *Engine) LocalHash() (string, error) {
	path, err := e.collectionPath()
	if err != nil {
		return "", err
	}
	c, err := obs.LoadCollection(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// La comparaison se fait sur la collection telle qu'elle serait publiée :
	// jeton d'assets en place et réglages matériels neutralisés. Sans quoi le
	// simple fait d'avoir sa propre webcam passerait pour une modification.
	obs.Tokenize(c, e.Cfg.AssetsDir)
	stripLocal(c)
	data, err := c.Normalize()
	if err != nil {
		return "", err
	}
	return manifest.Sum(data), nil
}

// HasLocalChanges répond à la seule question qui compte pour un membre : ai-je
// retouché mes scènes sans les publier ?
func (e *Engine) HasLocalChanges() (bool, error) {
	if e.Cfg.AppliedHash == "" {
		return false, nil
	}
	current, err := e.LocalHash()
	if err != nil || current == "" {
		return false, err
	}
	return current != e.Cfg.AppliedHash, nil
}

// stripLocal remet à zéro les réglages propres à la machine, pour que deux
// postes produisent la même empreinte à partir des mêmes scènes.
func stripLocal(c *obs.Collection) {
	for _, s := range c.Sources() {
		if !obs.IsHardware(s.Type()) {
			continue
		}
		settings := s.Settings()
		if settings == nil {
			continue
		}
		for _, key := range obs.LocalKeysFor(s.Type()) {
			delete(settings, key)
		}
	}
}

func fileSHA(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func describe(m *manifest.Manifest) string {
	if m.Message == "" {
		return fmt.Sprintf("v%d par %s", m.Version, m.Author)
	}
	return fmt.Sprintf("v%d par %s — %s", m.Version, m.Author, m.Message)
}
