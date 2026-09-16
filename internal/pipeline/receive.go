package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gilforge/stream-team/internal/config"
	"github.com/gilforge/stream-team/internal/manifest"
	"github.com/gilforge/stream-team/internal/obs"
	"github.com/gilforge/stream-team/internal/storage"
)

// Receive applique l'état publié à cette machine. Appelé avant le lancement
// d'OBS, quand les fichiers sur le disque font foi.
func (e *Engine) Receive(ctx context.Context, progress func(string)) (*Report, error) {
	if progress == nil {
		progress = func(string) {}
	}
	rep := &Report{}

	progress("Lecture du manifeste…")
	m, err := e.RemoteManifest(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf(
			"la régie %s ne contient encore aucun manifeste — "+
				"le responsable de l'équipe doit publier une première fois", e.Cfg.ReadURL)
	}
	if err != nil {
		return nil, err
	}
	rep.Version, rep.Author, rep.Message = m.Version, m.Author, m.Message

	if m.Version == e.Cfg.AppliedVersion {
		changed, err := e.HasLocalChanges()
		if err != nil {
			return nil, err
		}
		if !changed {
			rep.UpToDate = true
			progress("Déjà à jour (" + describe(m) + ")")
			return rep, nil
		}
	}

	collectionPath, err := e.collectionPath()
	if err != nil {
		return nil, err
	}

	progress("Sauvegarde de la collection actuelle…")
	if err := obs.Backup(collectionPath, filepath.Join(e.Dir, config.DirBackups), BackupsKept); err != nil {
		return nil, fmt.Errorf("sauvegarde impossible, réception annulée : %w", err)
	}

	// Les assets d'abord : si le téléchargement échoue à mi-parcours, la
	// collection locale n'a pas encore bougé et la session reste utilisable.
	for _, f := range m.Files {
		if f.Path == manifest.CollectionPath {
			continue
		}
		dest := e.assetPath(f.Path)
		if sum, size, err := fileSHA(dest); err == nil && sum == f.SHA256 && size == f.Size {
			rep.AssetsSkipped++
			continue
		}
		progress("Téléchargement " + filepath.Base(f.Path) + "…")
		if err := e.Reader.Download(ctx, f.Path, dest); err != nil {
			return nil, err
		}
		if sum, _, err := fileSHA(dest); err != nil || sum != f.SHA256 {
			os.Remove(dest)
			return nil, fmt.Errorf("%s est arrivé corrompu — réessayez", f.Path)
		}
		rep.AssetsFetched++
	}

	progress("Application des scènes…")
	raw, err := e.Reader.Get(ctx, manifest.CollectionPath)
	if err != nil {
		return nil, err
	}
	if entry, ok := m.Find(manifest.CollectionPath); ok {
		if manifest.Sum(raw) != entry.SHA256 {
			return nil, fmt.Errorf("la collection reçue ne correspond pas à son empreinte — publication en cours côté serveur ? réessayez dans un instant")
		}
	}

	c, err := obs.Decode(raw)
	if err != nil {
		return nil, err
	}

	// Réécriture des chemins vers le dossier d'assets de cette machine, puis
	// réinjection de ses périphériques. Sans cette seconde étape, chaque
	// réception remettrait la webcam du publieur et donnerait un écran noir.
	obs.Expand(c, e.Cfg.AssetsDir)
	rep.Unconfigured = obs.Apply(c, e.Overrides.Sources)

	if err := obs.SaveCollection(collectionPath, c); err != nil {
		return nil, err
	}

	// Le canvas est le seul réglage de profil imposé : des résolutions
	// différentes décaleraient toutes les positions de sources.
	if e.Cfg.Profile != "" && m.Canvas.BaseCX > 0 {
		profileDir, err := e.Paths.ProfileDir(e.Cfg.Profile)
		if err != nil {
			progress("Profil « " + e.Cfg.Profile + " » introuvable, canvas non appliqué")
		} else {
			changed, err := obs.ApplyCanvas(profileDir, obs.Canvas{
				BaseCX:   m.Canvas.BaseCX,
				BaseCY:   m.Canvas.BaseCY,
				OutputCX: m.Canvas.OutputCX,
				OutputCY: m.Canvas.OutputCY,
				FPS:      m.Canvas.FPS,
			})
			if err != nil {
				return nil, err
			}
			rep.CanvasChanged = changed
		}
	}

	hash, err := e.LocalHash()
	if err != nil {
		return nil, err
	}
	e.Cfg.AppliedVersion = m.Version
	e.Cfg.AppliedHash = hash
	if err := e.Cfg.Save(e.Dir); err != nil {
		return nil, err
	}

	progress("À jour : " + describe(m))
	return rep, nil
}

// CaptureDevices relève les réglages matériels de cette machine et les mémorise.
// Appelé après la fermeture d'OBS : si un membre a changé de webcam pendant sa
// session, le nouveau réglage est capturé sans qu'il ait rien à déclarer.
func (e *Engine) CaptureDevices() error {
	path, err := e.collectionPath()
	if err != nil {
		return err
	}
	c, err := obs.LoadCollection(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	e.Overrides.Sources = obs.Merge(e.Overrides.Sources, obs.Extract(c))
	return e.Overrides.Save(e.Dir)
}
