package obs

import (
	"strings"
	"testing"
)

func TestTokenizeExpandAllerRetour(t *testing.T) {
	const monDossier = `C:\StreamAssets`
	const sonDossier = "D:/Overlays/equipe"

	c, err := Decode([]byte(`{
	  "sources": [
	    { "id": "image_source", "name": "Cadre",
	      "settings": { "file": "C:/StreamAssets/cadre.png" } },
	    { "id": "ffmpeg_source", "name": "Stinger",
	      "settings": { "local_file": "C:/StreamAssets/videos/transition.mp4" } },
	    { "id": "text_gdiplus", "name": "Titre",
	      "settings": { "text": "Bienvenue", "font": { "face": "Archivo" } } }
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	if n := Tokenize(c, monDossier); n != 2 {
		t.Fatalf("2 chemins à tokeniser, %d traités", n)
	}
	normalisée, _ := c.Normalize()
	if strings.Contains(string(normalisée), "StreamAssets") {
		t.Error("aucun chemin local ne doit subsister dans ce qui part sur la régie")
	}
	if !strings.Contains(string(normalisée), AssetsToken+"/cadre.png") {
		t.Error("le chemin devait être remplacé par le jeton")
	}
	// Le texte d'une source n'est pas un chemin : il ne doit pas bouger.
	if !strings.Contains(string(normalisée), "Bienvenue") {
		t.Error("le contenu textuel a été altéré")
	}

	// Chez un membre dont le dossier s'appelle autrement, l'expansion doit
	// viser son propre emplacement.
	if n := Expand(c, sonDossier); n != 2 {
		t.Fatalf("2 chemins à étendre, %d traités", n)
	}
	chezLui, _ := c.Normalize()
	if !strings.Contains(string(chezLui), sonDossier+"/cadre.png") {
		t.Errorf("le jeton devait pointer vers le dossier local ; obtenu %s", chezLui)
	}
	if strings.Contains(string(chezLui), AssetsToken) {
		t.Error("il ne doit plus rester de jeton après expansion")
	}
}

func TestTokenizeIgnoreLaCasseDesLecteurs(t *testing.T) {
	// Windows ne distingue pas la casse : un chemin écrit « c:/ » par OBS doit
	// être reconnu face à un dossier configuré « C:\ ».
	c, err := Decode([]byte(`{"sources":[{"id":"image_source","name":"I",
	  "settings":{"file":"c:/streamassets/fond.png"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := Tokenize(c, `C:\StreamAssets`); n != 1 {
		t.Fatalf("le chemin devait être reconnu malgré la casse, %d traité", n)
	}
}

func TestMissingAssetsListeCeQuiResteTokenisé(t *testing.T) {
	c, _ := Decode([]byte(`{"sources":[
	  {"id":"image_source","name":"A","settings":{"file":"$ASSETS$/a.png"}},
	  {"id":"image_source","name":"B","settings":{"file":"C:/local/b.png"}}
	]}`))
	got := MissingAssets(c)
	if len(got) != 1 || !strings.Contains(got[0], "a.png") {
		t.Errorf("un seul chemin tokenisé attendu, obtenu %v", got)
	}
}
