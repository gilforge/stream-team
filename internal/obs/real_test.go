package obs

import (
	"os"
	"path/filepath"
	"testing"
)

// Ces tests confrontent le code aux collections réellement présentes sur la
// machine. Ils ne modifient rien et se sautent si OBS n'est pas installé, pour
// rester exécutables en intégration continue.
func collectionsRéelles(t *testing.T) []string {
	t.Helper()
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Skip("pas de dossier APPDATA sur cette plateforme")
	}
	dir := filepath.Join(appData, "obs-studio", "basic", "scenes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skip("aucune installation d'OBS sur cette machine")
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	if len(out) == 0 {
		t.Skip("aucune collection à examiner")
	}
	return out
}

func TestCollectionsRéellesSeDécodentEtSeStabilisent(t *testing.T) {
	for _, path := range collectionsRéelles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			c, err := Decode(data)
			if err != nil {
				t.Fatalf("décodage impossible : %v", err)
			}

			une, err := c.Normalize()
			if err != nil {
				t.Fatalf("normalisation : %v", err)
			}
			// Un second passage doit rendre exactement les mêmes octets, sans
			// quoi la détection de divergence signalerait une modification à
			// chaque session.
			relu, err := Decode(une)
			if err != nil {
				t.Fatalf("la forme normalisée doit rester lisible : %v", err)
			}
			deux, _ := relu.Normalize()
			if string(une) != string(deux) {
				t.Error("la normalisation n'est pas stable sur cette collection")
			}
		})
	}
}

func TestExtractSurCollectionsRéelles(t *testing.T) {
	trouvés := 0
	for _, path := range collectionsRéelles(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		c, err := Decode(data)
		if err != nil {
			continue
		}
		for nom, réglages := range Extract(c) {
			trouvés++
			if len(réglages) == 0 {
				t.Errorf("%s : la source %q est relevée sans aucun réglage", filepath.Base(path), nom)
			}
		}
	}
	if trouvés == 0 {
		t.Skip("aucune source matérielle dans les collections de cette machine")
	}
	t.Logf("%d sources matérielles reconnues dans les collections réelles", trouvés)
}

func TestAllerRetourCompletSurCollectionsRéelles(t *testing.T) {
	// Le cycle d'une publication suivie d'une réception : tokeniser, retirer
	// les appareils, puis étendre et réinjecter doit rendre la collection de
	// départ, à l'identique.
	for _, path := range collectionsRéelles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			départ, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			attendu, _ := départ.Normalize()

			travail, _ := Decode(data)
			appareils := Extract(travail)

			// Ce qui partirait sur la régie.
			Tokenize(travail, `C:\StreamAssets`)
			StripLocal(travail)

			// Ce qui reviendrait chez le même membre.
			Expand(travail, `C:\StreamAssets`)
			Apply(travail, appareils)

			obtenu, _ := travail.Normalize()
			if string(attendu) != string(obtenu) {
				t.Error("un aller-retour publication/réception a modifié la collection")
			}
		})
	}
}

// TestChemainsÉtrangersSurCollectionsRéelles n'échoue jamais : il renseigne.
// Il dit combien de fichiers d'une collection réelle échapperaient au dossier
// d'assets, et devraient donc y être déplacés avant de publier.
func TestCheminsÉtrangersSurCollectionsRéelles(t *testing.T) {
	const dossierAssets = `C:\StreamAssets`

	for _, path := range collectionsRéelles(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		c, err := Decode(data)
		if err != nil {
			continue
		}
		étrangers := ForeignPaths(c, dossierAssets)
		if len(étrangers) == 0 {
			continue
		}
		t.Logf("%s : %d chemin(s) hors du dossier d'assets", filepath.Base(path), len(étrangers))
		for i, p := range étrangers {
			if i == 8 {
				t.Logf("    … et %d autres", len(étrangers)-8)
				break
			}
			t.Logf("    %s", p)
		}
	}
}
