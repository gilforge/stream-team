package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gilforge/stream-team/internal/manifest"
	"github.com/gilforge/stream-team/internal/obs"
	"github.com/gilforge/stream-team/internal/storage"
)

// Publish dépose l'état local sur la régie. Appelé après la fermeture d'OBS,
// quand il vient d'écrire ses fichiers : c'est le seul moment où les lire a un
// sens.
//
// L'ordre d'écriture fait toute la cohérence. Sans WebDAV, on n'a ni écriture
// conditionnelle ni copie côté serveur ; la garantie vient de ce que le
// manifeste part en dernier. Tant qu'il n'a pas changé, les autres postes
// continuent de voir la version précédente, même si les nouveaux fichiers sont
// déjà en ligne.
func (e *Engine) Publish(ctx context.Context, message string, progress func(string)) (*Report, error) {
	if progress == nil {
		progress = func(string) {}
	}
	if e.Pub == nil {
		return nil, fmt.Errorf("ce poste est en lecture seule : aucun publisher.json n'accompagne l'exécutable")
	}
	rep := &Report{}

	collectionPath, err := e.collectionPath()
	if err != nil {
		return nil, err
	}
	c, err := obs.LoadCollection(collectionPath)
	if err != nil {
		return nil, fmt.Errorf("collection locale illisible : %w", err)
	}

	// Ce qui part sur le serveur ne contient ni chemin absolu ni identifiant
	// d'appareil : ce sont les deux choses qui ne valent que sur cette machine.
	obs.Tokenize(c, e.Cfg.AssetsDir)
	obs.StripLocal(c)
	payload, err := c.Normalize()
	if err != nil {
		return nil, err
	}
	hash := manifest.Sum(payload)

	if hash == e.Cfg.AppliedHash {
		rep.UpToDate = true
		progress("Aucune modification à publier")
		return rep, nil
	}

	progress("Vérification de la version en ligne…")
	remote, err := e.RemoteManifest(ctx)
	if err != nil {
		return nil, err
	}
	if remote.Version != e.Cfg.AppliedVersion {
		return nil, fmt.Errorf(
			"la régie est en v%d publiée par %s, alors que vous êtes parti de la v%d — "+
				"relancez pour recevoir avant de republier",
			remote.Version, remote.Author, e.Cfg.AppliedVersion)
	}

	canvas := remote.Canvas
	if e.Cfg.Profile != "" {
		if profileDir, err := e.Paths.ProfileDir(e.Cfg.Profile); err == nil {
			if local, err := obs.ReadCanvas(profileDir); err == nil && local.Valid() {
				canvas = manifest.Canvas{
					BaseCX: local.BaseCX, BaseCY: local.BaseCY,
					OutputCX: local.OutputCX, OutputCY: local.OutputCY,
					FPS: local.FPS,
				}
			}
		}
	}

	progress("Connexion au serveur…")
	writer, base, err := storage.NewWriter(ctx, e.Pub.WriteURL, storage.Credentials{
		Password:       e.Pub.Password,
		KeyFile:        e.Pub.KeyFile,
		KnownHostsFile: filepath.Join(e.Dir, "known-hosts.json"),
	})
	if err != nil {
		return nil, err
	}
	defer writer.Close()

	next := remote.Version + 1

	progress("Archivage de la version précédente…")
	if err := e.archive(ctx, writer, base, remote); err != nil {
		// L'archivage est un filet de sécurité, pas une étape critique : mieux
		// vaut publier sans que de refuser la publication.
		progress("Archivage impossible (" + err.Error() + "), publication poursuivie")
	}

	progress("Inventaire des assets…")
	files, err := e.scanAssets()
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		if prev, ok := remote.Find(f.Path); ok && prev.SHA256 == f.SHA256 {
			rep.AssetsSkipped++
			continue
		}
		progress("Envoi " + path.Base(f.Path) + "…")
		src, err := os.Open(e.assetPath(f.Path))
		if err != nil {
			return nil, err
		}
		err = writer.Put(ctx, joinRemote(base, f.Path), src)
		src.Close()
		if err != nil {
			return nil, err
		}
		rep.AssetsFetched++
	}

	progress("Envoi des scènes…")
	if err := writer.Put(ctx, joinRemote(base, manifest.CollectionPath), bytes.NewReader(payload)); err != nil {
		return nil, err
	}
	files = append(files, manifest.File{
		Path:   manifest.CollectionPath,
		Size:   int64(len(payload)),
		SHA256: hash,
	})

	m := &manifest.Manifest{
		Version: next,
		Author:  e.Pub.Author,
		Date:    time.Now().UTC(),
		Message: message,
		Canvas:  canvas,
		Files:   files,
	}
	data, err := m.Encode()
	if err != nil {
		return nil, err
	}

	// Le manifeste en dernier, et par fichier temporaire puis renommage : un
	// membre qui synchronise pendant la publication ne doit jamais tomber sur
	// un manifeste à moitié écrit.
	progress("Publication du manifeste…")
	tmpName := joinRemote(base, manifest.Name+".tmp")
	if err := writer.Put(ctx, tmpName, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	if err := writer.Move(ctx, tmpName, joinRemote(base, manifest.Name)); err != nil {
		return nil, err
	}

	e.Cfg.AppliedVersion = next
	e.Cfg.AppliedHash = hash
	if err := e.Cfg.Save(e.Dir); err != nil {
		return nil, err
	}

	rep.Published = true
	rep.Version = next
	rep.Author = e.Pub.Author
	rep.Message = message
	progress(fmt.Sprintf("Publié en v%d", next))
	return rep, nil
}

// archive recopie le manifeste et la collection en ligne dans versions/vN/.
//
// Faute de copie côté serveur, il faut les retélécharger puis les redéposer.
// C'est indolore sur deux fichiers JSON — et c'est justement pourquoi on
// n'archive pas les assets.
func (e *Engine) archive(ctx context.Context, w storage.Writer, base string, m *manifest.Manifest) error {
	dir := fmt.Sprintf("versions/v%d", m.Version)

	raw, err := m.Encode()
	if err != nil {
		return err
	}
	if err := w.Put(ctx, joinRemote(base, dir+"/"+manifest.Name), bytes.NewReader(raw)); err != nil {
		return err
	}

	collection, err := e.Reader.Get(ctx, manifest.CollectionPath)
	if err != nil {
		return err
	}
	return w.Put(ctx, joinRemote(base, dir+"/collection.json"), bytes.NewReader(collection))
}

// scanAssets inventorie le dossier d'assets local. On publie le dossier entier
// plutôt que les seuls fichiers référencés : c'est prévisible, et une image
// ajoutée pour la semaine prochaine part avec le reste.
func (e *Engine) scanAssets() ([]manifest.File, error) {
	root := e.Cfg.AssetsDir
	if root == "" {
		return nil, nil
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	var out []manifest.File
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		sum, size, err := fileSHA(p)
		if err != nil {
			return err
		}
		out = append(out, manifest.File{
			Path:   "assets/" + filepath.ToSlash(rel),
			Size:   size,
			SHA256: sum,
		})
		return nil
	})
	return out, err
}

func joinRemote(base, rel string) string {
	base = strings.TrimSuffix(base, "/")
	rel = strings.TrimPrefix(rel, "/")
	if base == "" {
		return rel
	}
	return base + "/" + rel
}
