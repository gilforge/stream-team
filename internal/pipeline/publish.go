package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
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

	// Ce qui reste en chemin absolu après tokenisation ne sera trouvable chez
	// personne. On publie quand même — c'est le choix du publieur — mais il
	// doit le savoir avant que l'équipe ne découvre des sources vides.
	rep.ForeignPaths = obs.ForeignPaths(c, e.Cfg.AssetsDir)
	for _, p := range rep.ForeignPaths {
		progress("Hors du dossier d'assets, introuvable chez les autres : " + p)
	}
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

	// Une régie sans manifeste n'est pas en panne : elle n'a simplement jamais
	// rien reçu. Cette publication l'amorce en v1.
	amorçage := errors.Is(err, storage.ErrNotFound)
	switch {
	case amorçage:
		progress("Régie vide : première publication")
		remote = &manifest.Manifest{Version: 0}
	case err != nil:
		return nil, err
	case remote.Version != e.Cfg.AppliedVersion:
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

	writer, base := e.Writer, ""
	if writer == nil {
		progress("Connexion au serveur…")
		writer, base, err = storage.NewWriter(ctx, e.Pub.WriteURL, storage.Credentials{
			Password:       e.Pub.Password,
			KeyFile:        e.Pub.KeyFile,
			KnownHostsFile: filepath.Join(e.Dir, "known-hosts.json"),
		})
		if err != nil {
			return nil, err
		}
		defer writer.Close()
	}

	next := remote.Version + 1

	if !amorçage {
		progress("Archivage de la version précédente…")
		if err := e.archive(ctx, writer, base, remote); err != nil {
			// L'archivage est un filet de sécurité, pas une étape critique :
			// mieux vaut publier que refuser la publication.
			progress("Archivage impossible (" + err.Error() + "), publication poursuivie")
		}
	}

	progress("Inventaire des assets…")
	files, introuvables, err := e.referencedAssets(c)
	if err != nil {
		return nil, err
	}
	for _, p := range introuvables {
		progress("Référencé mais absent du disque, non publié : " + p)
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

// referencedAssets n'inventorie que les fichiers dont la collection a besoin.
//
// Publier le dossier d'assets entier serait plus simple à expliquer, mais un
// dossier de travail contient des sources de montage, des archives et des
// fichiers de projet qui n'ont rien à faire sur la régie : on y enverrait des
// gigaoctets que personne ne téléchargera. La liste des chemins tokenisés dit
// exactement ce qui est utile.
//
// Renvoie aussi les fichiers référencés mais absents du disque : l'auteur doit
// le savoir avant que l'équipe ne découvre des sources vides.
func (e *Engine) referencedAssets(c *obs.Collection) ([]manifest.File, []string, error) {
	vus := map[string]bool{}
	var out []manifest.File
	var introuvables []string

	for _, ref := range obs.MissingAssets(c) {
		rel := strings.TrimPrefix(strings.TrimPrefix(ref, obs.AssetsToken), "/")
		if rel == "" || vus[rel] {
			continue
		}
		vus[rel] = true

		local := filepath.Join(e.Cfg.AssetsDir, filepath.FromSlash(rel))
		sum, size, err := fileSHA(local)
		if err != nil {
			if os.IsNotExist(err) {
				introuvables = append(introuvables, rel)
				continue
			}
			return nil, nil, err
		}
		out = append(out, manifest.File{
			Path:   "assets/" + rel,
			Size:   size,
			SHA256: sum,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	sort.Strings(introuvables)
	return out, introuvables, nil
}

func joinRemote(base, rel string) string {
	base = strings.TrimSuffix(base, "/")
	rel = strings.TrimPrefix(rel, "/")
	if base == "" {
		return rel
	}
	return base + "/" + rel
}
