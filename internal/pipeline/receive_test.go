package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gilforge/stream-team/internal/config"
	"github.com/gilforge/stream-team/internal/manifest"
	"github.com/gilforge/stream-team/internal/obs"
	"github.com/gilforge/stream-team/internal/storage"
)

// La collection telle qu'elle est publiée : chemins tokenisés et appareils
// absents, puisque ni les uns ni les autres ne valent hors de la machine qui a
// publié.
const collectionPubliée = `{
  "name": "Équipe",
  "current_scene": "Live",
  "sources": [
    { "id": "scene", "name": "Live", "settings": { "items": [] } },
    { "id": "dshow_input", "name": "Webcam", "settings": { "resolution": "1920x1080" } },
    { "id": "wasapi_input_capture", "name": "Micro", "settings": {} },
    { "id": "image_source", "name": "Habillage",
      "settings": { "file": "$ASSETS$/habillage.png" } }
  ]
}`

const profilLocal = `[General]
Name=Équipe

[SimpleOutput]
StreamEncoder=jim_nvenc
FilePath=D:\Enregistrements

[Video]
BaseCX=1920
BaseCY=1080
OutputCX=1920
OutputCY=1080
FPSType=0
FPSCommon=30
`

// banc monte une régie servie en HTTP statique — exactement ce que fournit un
// hébergement web ordinaire — et un faux dossier de configuration OBS.
type banc struct {
	engine     *Engine
	scenesDir  string
	profileDir string
	assetsDir  string
	régieDir   string
	url        string
}

// monterBancSurRégie ajoute un second poste sur la même régie : autre machine,
// autre matériel, autre dossier d'overlays. C'est ce qui permet de vérifier
// qu'une publication traverse correctement jusqu'à un équipier.
func monterBancSurRégie(t *testing.T, premier *banc) *banc {
	t.Helper()

	obsRoot := t.TempDir()
	scenesDir := filepath.Join(obsRoot, "basic", "scenes")
	profileDir := filepath.Join(obsRoot, "basic", "profiles", "Equipe")
	for _, d := range []string{scenesDir, profileDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	écrire(t, filepath.Join(profileDir, "basic.ini"), []byte(profilLocal))

	reader, err := storage.NewHTTPReader(premier.url)
	if err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(t.TempDir(), "MesOverlays")

	return &banc{
		scenesDir:  scenesDir,
		profileDir: profileDir,
		assetsDir:  assetsDir,
		régieDir:   premier.régieDir,
		url:        premier.url,
		engine: &Engine{
			Dir:   t.TempDir(),
			Paths: &obs.Paths{Root: obsRoot, Scenes: scenesDir, Profiles: filepath.Join(obsRoot, "basic", "profiles")},
			Cfg: &config.Config{
				ReadURL:    premier.url,
				AssetsDir:  assetsDir,
				Collection: "Équipe",
				Profile:    "Équipe",
				DockPort:   47839,
			},
			Reader: reader,
			Overrides: &config.Overrides{Sources: map[string]map[string]any{
				"Webcam": {"video_device_id": "SA_WEBCAM_A_LUI"},
			}},
		},
	}
}

// remplacerManifeste simule la publication d'un tiers pendant qu'on travaillait.
func remplacerManifeste(t *testing.T, régieDir string, version int, auteur string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(régieDir, manifest.Name))
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	m.Version = version
	m.Author = auteur
	raw, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	écrire(t, filepath.Join(régieDir, manifest.Name), raw)
}

func monterBanc(t *testing.T) *banc {
	t.Helper()

	// ---- côté OBS ----
	obsRoot := t.TempDir()
	scenesDir := filepath.Join(obsRoot, "basic", "scenes")
	profileDir := filepath.Join(obsRoot, "basic", "profiles", "Equipe")
	for _, d := range []string{scenesDir, profileDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	écrire(t, filepath.Join(profileDir, "basic.ini"), []byte(profilLocal))
	// Une collection déjà présente, pour vérifier qu'elle est sauvegardée.
	écrire(t, filepath.Join(scenesDir, "Equipe.json"),
		[]byte(`{"name":"Équipe","sources":[{"id":"scene","name":"Ancienne","settings":{}}]}`))

	// ---- côté régie ----
	régieDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(régieDir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(régieDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	habillage := []byte("PNG factice mais de contenu stable")
	écrire(t, filepath.Join(régieDir, "assets", "habillage.png"), habillage)
	écrire(t, filepath.Join(régieDir, "scenes", "collection.json"), []byte(collectionPubliée))

	m := &manifest.Manifest{
		Version: 7,
		Author:  "Marc",
		Date:    time.Now().UTC(),
		Message: "nouvel habillage",
		Canvas:  manifest.Canvas{BaseCX: 2560, BaseCY: 1440, OutputCX: 2560, OutputCY: 1440, FPS: "60"},
		Files: []manifest.File{
			{Path: "assets/habillage.png", Size: int64(len(habillage)), SHA256: manifest.Sum(habillage)},
			{Path: manifest.CollectionPath, Size: int64(len(collectionPubliée)), SHA256: manifest.Sum([]byte(collectionPubliée))},
		},
	}
	raw, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	écrire(t, filepath.Join(régieDir, manifest.Name), raw)

	srv := httptest.NewServer(http.FileServer(http.Dir(régieDir)))
	t.Cleanup(srv.Close)

	reader, err := storage.NewHTTPReader(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}

	assetsDir := filepath.Join(t.TempDir(), "StreamAssets")
	return &banc{
		scenesDir:  scenesDir,
		profileDir: profileDir,
		assetsDir:  assetsDir,
		régieDir:   régieDir,
		url:        srv.URL + "/",
		engine: &Engine{
			Dir:   t.TempDir(),
			Paths: &obs.Paths{Root: obsRoot, Scenes: scenesDir, Profiles: filepath.Join(obsRoot, "basic", "profiles")},
			Cfg: &config.Config{
				ReadURL:    srv.URL + "/",
				AssetsDir:  assetsDir,
				Collection: "Équipe",
				Profile:    "Équipe",
				DockPort:   47838,
			},
			Reader: reader,
			Overrides: &config.Overrides{Sources: map[string]map[string]any{
				"Webcam": {"video_device_id": "MA_WEBCAM_A_MOI"},
			}},
		},
	}
}

func écrire(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRéceptionDeBoutEnBout(t *testing.T) {
	b := monterBanc(t)
	e := b.engine

	rep, err := e.Receive(context.Background(), nil)
	if err != nil {
		t.Fatalf("réception : %v", err)
	}

	if rep.Version != 7 || rep.Author != "Marc" {
		t.Errorf("manifeste mal lu : v%d par %s", rep.Version, rep.Author)
	}
	if rep.AssetsFetched != 1 {
		t.Errorf("1 asset à télécharger, %d rapportés", rep.AssetsFetched)
	}

	// L'asset atterrit dans le dossier de cette machine, pas dans celui du
	// publieur.
	if _, err := os.Stat(filepath.Join(b.assetsDir, "habillage.png")); err != nil {
		t.Errorf("l'habillage n'est pas arrivé : %v", err)
	}

	// La collection précédente a été sauvegardée avant d'être remplacée.
	backups, _ := os.ReadDir(filepath.Join(e.Dir, config.DirBackups))
	if len(backups) != 1 {
		t.Errorf("une sauvegarde attendue, %d trouvée(s)", len(backups))
	}

	appliquée := lireCollection(t, b.scenesDir)

	// Les chemins pointent vers le dossier local.
	if !strings.Contains(appliquée, filepath.ToSlash(b.assetsDir)+"/habillage.png") {
		t.Error("le jeton $ASSETS$ n'a pas été réécrit vers le dossier local")
	}
	if strings.Contains(appliquée, obs.AssetsToken) {
		t.Error("un jeton subsiste dans la collection appliquée")
	}
	// La webcam de cette machine a été réinjectée.
	if !strings.Contains(appliquée, "MA_WEBCAM_A_MOI") {
		t.Error("l'appareil local n'a pas été réinjecté")
	}
	// Le micro n'a jamais été configuré ici : il doit être signalé.
	if len(rep.Unconfigured) != 1 || rep.Unconfigured[0] != "Micro" {
		t.Errorf("le micro devait être signalé à configurer, obtenu %v", rep.Unconfigured)
	}

	// Le canvas commun a été imposé, sans toucher à l'encodeur de la machine.
	profil, _ := os.ReadFile(filepath.Join(b.profileDir, "basic.ini"))
	if !strings.Contains(string(profil), "BaseCX=2560") || !strings.Contains(string(profil), "FPSCommon=60") {
		t.Errorf("canvas non appliqué :\n%s", profil)
	}
	if !strings.Contains(string(profil), "StreamEncoder=jim_nvenc") {
		t.Error("l'encodeur local a été écrasé")
	}
	if !rep.CanvasChanged {
		t.Error("le canvas a changé, le rapport devait le dire")
	}

	if e.Cfg.AppliedVersion != 7 || e.Cfg.AppliedHash == "" {
		t.Errorf("état non enregistré : v%d hash=%q", e.Cfg.AppliedVersion, e.Cfg.AppliedHash)
	}
}

func TestSecondeRéceptionNeRefaitRien(t *testing.T) {
	b := monterBanc(t)
	e := b.engine

	if _, err := e.Receive(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	rep, err := e.Receive(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if !rep.UpToDate {
		t.Error("rien n'a changé entre les deux : la seconde réception devait s'arrêter net")
	}
	if rep.AssetsFetched != 0 {
		t.Errorf("aucun asset à retélécharger, %d rapportés", rep.AssetsFetched)
	}

	// Et l'agent ne doit pas croire que le membre a modifié ses scènes.
	changé, err := e.HasLocalChanges()
	if err != nil {
		t.Fatal(err)
	}
	if changé {
		t.Error("une réception propre ne doit pas être prise pour une modification locale")
	}
}

func TestModificationLocaleDétectée(t *testing.T) {
	b := monterBanc(t)
	e := b.engine

	if _, err := e.Receive(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Le membre ajoute une source, comme OBS le ferait en se fermant.
	chemin := filepath.Join(b.scenesDir, "Equipe.json")
	c, err := obs.LoadCollection(chemin)
	if err != nil {
		t.Fatal(err)
	}
	sources := c.Data["sources"].([]any)
	c.Data["sources"] = append(sources, map[string]any{
		"id": "color_source", "name": "Fond", "settings": map[string]any{},
	})
	if err := obs.SaveCollection(chemin, c); err != nil {
		t.Fatal(err)
	}

	changé, err := e.HasLocalChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !changé {
		t.Error("l'ajout d'une source devait être vu comme une modification à publier")
	}
}

func TestChangerDeWebcamNestPasUneModification(t *testing.T) {
	b := monterBanc(t)
	e := b.engine

	if _, err := e.Receive(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Le membre change de webcam dans OBS : son matériel lui appartient, ça ne
	// doit rien déclencher vis-à-vis de l'équipe.
	chemin := filepath.Join(b.scenesDir, "Equipe.json")
	c, _ := obs.LoadCollection(chemin)
	for _, s := range c.Sources() {
		if s.Name() == "Webcam" {
			s.EnsureSettings()["video_device_id"] = "UNE_AUTRE_CAMERA"
		}
	}
	if err := obs.SaveCollection(chemin, c); err != nil {
		t.Fatal(err)
	}

	changé, err := e.HasLocalChanges()
	if err != nil {
		t.Fatal(err)
	}
	if changé {
		t.Error("changer de webcam est un fait local, pas une modification des scènes de l'équipe")
	}
}

func TestManifesteDemandéSansCache(t *testing.T) {
	var requêtes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requêtes = append(requêtes, r.URL.String()+"|"+r.Header.Get("Cache-Control"))
		http.NotFound(w, r)
	}))
	defer srv.Close()

	reader, _ := storage.NewHTTPReader(srv.URL + "/")
	e := &Engine{Cfg: &config.Config{}, Reader: reader}
	_, _ = e.RemoteManifest(context.Background())

	if len(requêtes) != 1 {
		t.Fatalf("une requête attendue, %d reçues", len(requêtes))
	}
	if !strings.Contains(requêtes[0], "no-cache") {
		t.Errorf("le manifeste doit être demandé sans cache : %s", requêtes[0])
	}
	if !strings.Contains(requêtes[0], "?_=") {
		t.Errorf("un paramètre anti-cache est attendu dans l'URL : %s", requêtes[0])
	}
}

func lireCollection(t *testing.T, scenesDir string) string {
	t.Helper()
	entries, err := os.ReadDir(scenesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(scenesDir, e.Name()))
		if err != nil {
			continue
		}
		var head struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &head) == nil && head.Name == "Équipe" {
			return string(data)
		}
	}
	t.Fatal("collection appliquée introuvable")
	return ""
}
