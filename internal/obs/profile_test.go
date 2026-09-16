package obs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// basicINI reprend la forme d'un vrai basic.ini : la section [Video] qu'on
// modifie, entourée de sections qui ne doivent pas bouger.
const basicINI = `[General]
Name=Équipe

[SimpleOutput]
StreamEncoder=jim_nvenc
VBitrate=6000
FilePath=D:\Enregistrements

[Video]
BaseCX=1920
BaseCY=1080
OutputCX=1280
OutputCY=720
FPSType=0
FPSCommon=30

[Audio]
SampleRate=48000
`

func écrireProfil(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "basic.ini"), []byte(basicINI), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestApplyCanvasNeToucheQueLaVidéo(t *testing.T) {
	dir := écrireProfil(t)

	changé, err := ApplyCanvas(dir, Canvas{
		BaseCX: 2560, BaseCY: 1440,
		OutputCX: 2560, OutputCY: 1440,
		FPS: "60",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changé {
		t.Error("les valeurs différaient, le fichier devait être modifié")
	}

	got, _ := os.ReadFile(filepath.Join(dir, "basic.ini"))
	contenu := string(got)

	// L'encodeur et le chemin d'enregistrement dépendent de la machine :
	// remplacer le profil entier les effacerait.
	for _, intact := range []string{
		"StreamEncoder=jim_nvenc",
		`FilePath=D:\Enregistrements`,
		"SampleRate=48000",
		"Name=Équipe",
	} {
		if !strings.Contains(contenu, intact) {
			t.Errorf("%q a disparu du profil", intact)
		}
	}
	for _, attendu := range []string{"BaseCX=2560", "BaseCY=1440", "OutputCX=2560", "FPSCommon=60"} {
		if !strings.Contains(contenu, attendu) {
			t.Errorf("%q manque dans le profil", attendu)
		}
	}
}

func TestApplyCanvasIdentiqueNÉcritPas(t *testing.T) {
	dir := écrireProfil(t)
	actuel, err := ReadCanvas(dir)
	if err != nil {
		t.Fatal(err)
	}

	changé, err := ApplyCanvas(dir, actuel)
	if err != nil {
		t.Fatal(err)
	}
	if changé {
		t.Error("appliquer les valeurs déjà en place ne doit rien réécrire")
	}
}

func TestApplyCanvasAjouteLesClésAbsentes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "basic.ini"), []byte("[General]\nName=X\n"), 0o644)

	if _, err := ApplyCanvas(dir, Canvas{
		BaseCX: 1920, BaseCY: 1080, OutputCX: 1920, OutputCY: 1080, FPS: "60",
	}); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "basic.ini"))
	contenu := string(got)
	if !strings.Contains(contenu, "[Video]") || !strings.Contains(contenu, "BaseCX=1920") {
		t.Errorf("la section [Video] devait être créée :\n%s", contenu)
	}
	if !strings.Contains(contenu, "Name=X") {
		t.Error("la section existante a été perdue")
	}
}

func TestReadCanvas(t *testing.T) {
	dir := écrireProfil(t)
	c, err := ReadCanvas(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseCX != 1920 || c.OutputCY != 720 || c.FPS != "30" {
		t.Errorf("lecture incorrecte : %s", c)
	}
	if !c.Valid() {
		t.Error("ce canvas est complet, il devait être valide")
	}
}
