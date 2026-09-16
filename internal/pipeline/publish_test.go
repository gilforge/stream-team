package pipeline

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gilforge/stream-team/internal/config"
	"github.com/gilforge/stream-team/internal/manifest"
	"github.com/gilforge/stream-team/internal/obs"
)

// écrivainLocal remplace la connexion FTP par des écritures dans un dossier.
// Comme ce dossier est justement celui que sert le serveur HTTP du banc, une
// publication devient immédiatement visible en lecture : on peut donc éprouver
// le cycle complet publier puis recevoir.
type écrivainLocal struct {
	racine string
	écrits []string
}

func (w *écrivainLocal) Put(_ context.Context, p string, r io.Reader) error {
	dest := filepath.Join(w.racine, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	w.écrits = append(w.écrits, strings.TrimPrefix(p, "/"))
	return os.WriteFile(dest, data, 0o644)
}

func (w *écrivainLocal) Mkdir(_ context.Context, dir string) error {
	return os.MkdirAll(filepath.Join(w.racine, filepath.FromSlash(dir)), 0o755)
}

func (w *écrivainLocal) Move(_ context.Context, from, to string) error {
	w.écrits = append(w.écrits, "MOVE "+from+" -> "+to)
	return os.Rename(
		filepath.Join(w.racine, filepath.FromSlash(from)),
		filepath.Join(w.racine, filepath.FromSlash(to)))
}

func (w *écrivainLocal) Close() error { return nil }

func armerPublieur(b *banc) *écrivainLocal {
	w := &écrivainLocal{racine: b.régieDir}
	b.engine.Pub = &config.Publisher{WriteURL: "ftps://marc@exemple.fr/www", Author: "Gilles"}
	b.engine.Writer = w
	return w
}

func TestPublicationAprèsModification(t *testing.T) {
	b := monterBanc(t)
	e := b.engine
	w := armerPublieur(b)

	if _, err := e.Receive(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Le membre ajoute une source, comme OBS l'écrirait en se fermant.
	chemin := filepath.Join(b.scenesDir, "Equipe.json")
	c, _ := obs.LoadCollection(chemin)
	c.Data["sources"] = append(c.Data["sources"].([]any), map[string]any{
		"id": "color_source", "name": "Fond", "settings": map[string]any{},
	})
	if err := obs.SaveCollection(chemin, c); err != nil {
		t.Fatal(err)
	}

	rep, err := e.Publish(context.Background(), "ajout d'un fond", nil)
	if err != nil {
		t.Fatalf("publication : %v", err)
	}
	if !rep.Published || rep.Version != 8 {
		t.Fatalf("v8 attendue, obtenu v%d (publié=%v)", rep.Version, rep.Published)
	}

	// Le manifeste part en dernier, et par fichier temporaire puis renommage :
	// c'est ce qui tient lieu d'atomicité faute d'écriture conditionnelle.
	dernier := w.écrits[len(w.écrits)-1]
	if !strings.HasPrefix(dernier, "MOVE") || !strings.HasSuffix(dernier, manifest.Name) {
		t.Errorf("la dernière écriture devait publier le manifeste, obtenu %q", dernier)
	}
	avantDernier := w.écrits[len(w.écrits)-2]
	if !strings.Contains(avantDernier, manifest.Name+".tmp") {
		t.Errorf("le manifeste devait passer par un fichier temporaire, obtenu %q", avantDernier)
	}

	// La version précédente a été archivée.
	if _, err := os.Stat(filepath.Join(b.régieDir, "versions", "v7", manifest.Name)); err != nil {
		t.Errorf("la v7 devait être archivée : %v", err)
	}

	// Ce qui est publié ne doit porter ni chemin local ni appareil.
	publié, err := os.ReadFile(filepath.Join(b.régieDir, "scenes", "collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publié), "MA_WEBCAM_A_MOI") {
		t.Error("l'appareil de cette machine est parti sur la régie")
	}
	if strings.Contains(string(publié), b.assetsDir) {
		t.Error("un chemin local est parti sur la régie")
	}
	if !strings.Contains(string(publié), obs.AssetsToken) {
		t.Error("les chemins devaient être tokenisés avant publication")
	}
	if !strings.Contains(string(publié), `"Fond"`) {
		t.Error("la source ajoutée n'a pas été publiée")
	}
}

func TestPublicationSansModificationNeFaitRien(t *testing.T) {
	b := monterBanc(t)
	e := b.engine
	w := armerPublieur(b)

	if _, err := e.Receive(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	rep, err := e.Publish(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.UpToDate || rep.Published {
		t.Error("rien n'a changé : il ne devait y avoir aucune publication")
	}
	if len(w.écrits) != 0 {
		t.Errorf("aucune écriture attendue, obtenu %v", w.écrits)
	}
}

func TestPublicationDepuisUneVersionPériméeEstRefusée(t *testing.T) {
	b := monterBanc(t)
	e := b.engine
	armerPublieur(b)

	if _, err := e.Receive(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Quelqu'un d'autre publie entre-temps.
	remplacerManifeste(t, b.régieDir, 9, "Sophie")

	chemin := filepath.Join(b.scenesDir, "Equipe.json")
	c, _ := obs.LoadCollection(chemin)
	c.Data["sources"] = append(c.Data["sources"].([]any), map[string]any{
		"id": "color_source", "name": "Tardif", "settings": map[string]any{},
	})
	obs.SaveCollection(chemin, c)

	_, err := e.Publish(context.Background(), "trop tard", nil)
	if err == nil {
		t.Fatal("publier par-dessus une version plus récente doit être refusé")
	}
	if !strings.Contains(err.Error(), "Sophie") || !strings.Contains(err.Error(), "v9") {
		t.Errorf("le refus doit dire qui a publié quoi, obtenu : %v", err)
	}
}

func TestPremièrePublicationSurRégieVide(t *testing.T) {
	b := monterBanc(t)
	e := b.engine
	w := armerPublieur(b)

	// Régie encore vide : ni manifeste ni scènes. C'est l'état d'un dossier
	// qu'on vient de créer chez son hébergeur.
	os.Remove(filepath.Join(b.régieDir, manifest.Name))
	os.RemoveAll(filepath.Join(b.régieDir, "scenes"))

	// La réception doit le dire clairement plutôt que d'échouer sèchement.
	_, err := e.Receive(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "aucun manifeste") {
		t.Fatalf("message attendu sur la régie vide, obtenu : %v", err)
	}

	rep, err := e.Publish(context.Background(), "mise en place", nil)
	if err != nil {
		t.Fatalf("la première publication doit amorcer la régie : %v", err)
	}
	if rep.Version != 1 {
		t.Errorf("une régie vide s'amorce en v1, obtenu v%d", rep.Version)
	}
	for _, écrit := range w.écrits {
		if strings.Contains(écrit, "versions/") {
			t.Errorf("il n'y a rien à archiver sur une régie vide : %s", écrit)
		}
	}
	if _, err := os.Stat(filepath.Join(b.régieDir, manifest.Name)); err != nil {
		t.Errorf("le manifeste devait être créé : %v", err)
	}
}

func TestCyclePublierPuisRecevoirChezUnAutreMembre(t *testing.T) {
	b := monterBanc(t)
	publieur := b.engine
	armerPublieur(b)

	if _, err := publieur.Receive(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	chemin := filepath.Join(b.scenesDir, "Equipe.json")
	c, _ := obs.LoadCollection(chemin)
	c.Data["sources"] = append(c.Data["sources"].([]any), map[string]any{
		"id": "text_gdiplus", "name": "Titre", "settings": map[string]any{"text": "En direct"},
	})
	obs.SaveCollection(chemin, c)

	if _, err := publieur.Publish(context.Background(), "nouveau titre", nil); err != nil {
		t.Fatal(err)
	}

	// Un second membre, avec son propre matériel et son propre dossier
	// d'overlays, reçoit ce qui vient d'être publié.
	autre := monterBancSurRégie(t, b)
	rep, err := autre.engine.Receive(context.Background(), nil)
	if err != nil {
		t.Fatalf("réception chez le second membre : %v", err)
	}
	if rep.Version != 8 {
		t.Errorf("le second membre devait recevoir la v8, obtenu v%d", rep.Version)
	}

	reçue := lireCollection(t, autre.scenesDir)
	if !strings.Contains(reçue, "En direct") {
		t.Error("la modification du publieur n'est pas arrivée")
	}
	// Son matériel à lui, pas celui du publieur.
	if strings.Contains(reçue, "MA_WEBCAM_A_MOI") {
		t.Error("le second membre a hérité de la webcam du publieur")
	}
	if !strings.Contains(reçue, "SA_WEBCAM_A_LUI") {
		t.Error("le second membre n'a pas retrouvé sa propre webcam")
	}
	// Ses chemins à lui.
	if !strings.Contains(reçue, filepath.ToSlash(autre.assetsDir)) {
		t.Error("les chemins ne pointent pas vers le dossier du second membre")
	}
}
