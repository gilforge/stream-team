package obs

import (
	"strings"
	"testing"
)

func TestNormalizePréserveLesNombresExactement(t *testing.T) {
	// Sans UseNumber au décodage, tous ces nombres deviendraient des float64.
	// Une couleur sur 32 bits ou un identifiant long ressortirait en notation
	// scientifique, et OBS relirait un fichier différent du sien.
	const src = `{"color":4294967295,"id":9007199254740993,"volume":1.0,"ratio":0.1,"delai":0}`

	c, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	for _, littéral := range []string{"4294967295", "9007199254740993", "1.0", "0.1"} {
		if !strings.Contains(got, littéral) {
			t.Errorf("le nombre %s a été altéré ; obtenu %s", littéral, got)
		}
	}
	if strings.Contains(got, "e+") {
		t.Errorf("un nombre est passé en notation scientifique : %s", got)
	}
}

func TestNormalizeEstDéterministe(t *testing.T) {
	// OBS réécrit sa collection à sa façon à chaque fermeture : ordre des clés
	// et indentation lui appartiennent. La détection de divergence n'a de sens
	// que si deux contenus identiques produisent les mêmes octets.
	a, _ := Decode([]byte(`{"b":2,"a":1,"c":{"z":1,"y":2}}`))
	b, _ := Decode([]byte("{\n  \"c\": {\"y\": 2, \"z\": 1},\n  \"a\": 1,\n  \"b\": 2\n}"))

	na, _ := a.Normalize()
	nb, _ := b.Normalize()

	if string(na) != string(nb) {
		t.Errorf("deux écritures du même contenu doivent normaliser pareil :\n%s\n%s", na, nb)
	}
}

func TestNormalizeNÉchappePasLesURL(t *testing.T) {
	// L'échappement HTML par défaut de Go transformerait & en & dans les
	// URL des sources navigateur.
	c, _ := Decode([]byte(`{"settings":{"url":"https://x.fr/?a=1&b=2"}}`))
	out, _ := c.Normalize()
	if !strings.Contains(string(out), "a=1&b=2") {
		t.Errorf("l'URL a été échappée : %s", out)
	}
}

func TestSourcesLitTypeEtNom(t *testing.T) {
	c := decodeFixture(t)
	sources := c.Sources()
	if len(sources) != 5 {
		t.Fatalf("5 entrées attendues, %d lues", len(sources))
	}
	if sources[0].Type() != "scene" || sources[0].Name() != "Live" {
		t.Errorf("première entrée mal lue : %s / %s", sources[0].Type(), sources[0].Name())
	}
}

func TestEnsureSettingsCréeLeBlocManquant(t *testing.T) {
	c, _ := Decode([]byte(`{"sources":[{"id":"dshow_input","name":"Cam"}]}`))
	s := c.Sources()[0]
	if s.Settings() != nil {
		t.Fatal("cette source n'a pas de bloc settings")
	}
	s.EnsureSettings()["video_device_id"] = "X"

	out, _ := c.Normalize()
	if !strings.Contains(string(out), "video_device_id") {
		t.Errorf("le réglage devait être écrit dans la collection : %s", out)
	}
}
