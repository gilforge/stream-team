package obs

import "testing"

// collectionFixture reproduit la structure d'une vraie collection OBS : un
// tableau « sources » plat où scènes et sources se côtoient, chacune avec son
// type dans « id ». Les identifiants d'appareils sont repris tels qu'OBS les
// écrit réellement.
const collectionFixture = `{
  "name": "Équipe",
  "current_scene": "Live",
  "sources": [
    {
      "id": "scene",
      "name": "Live",
      "settings": { "items": [] }
    },
    {
      "id": "dshow_input",
      "name": "Webcam",
      "settings": {
        "video_device_id": "WEBCAM SKILLKORP:\\\\?\\usb#vid_1bcf&pid_2283#global",
        "last_video_device_id": "WEBCAM SKILLKORP:\\\\?\\usb#vid_1bcf&pid_2283#global",
        "resolution": "1920x1080",
        "fps": 30
      }
    },
    {
      "id": "wasapi_input_capture",
      "name": "Micro",
      "settings": {
        "device_id": "{0.0.1.00000000}.{bfd7b847-b3d4-46ac-921b-ee995c6614c8}",
        "use_device_timing": false
      }
    },
    {
      "id": "monitor_capture",
      "name": "Écran",
      "settings": { "monitor_id": "\\\\?\\DISPLAY#UGD1302#5&261ddd82" }
    },
    {
      "id": "image_source",
      "name": "Overlay",
      "settings": { "file": "C:/StreamAssets/cadre.png" }
    }
  ]
}`

func decodeFixture(t *testing.T) *Collection {
	t.Helper()
	c, err := Decode([]byte(collectionFixture))
	if err != nil {
		t.Fatalf("décodage : %v", err)
	}
	return c
}

func TestExtractNeRelèveQueLeMatériel(t *testing.T) {
	got := Extract(decodeFixture(t))

	if len(got) != 3 {
		t.Fatalf("3 sources matérielles attendues, %d relevées : %v", len(got), got)
	}
	if _, ok := got["Overlay"]; ok {
		t.Error("une source image ne dépend pas du matériel, elle ne doit pas être relevée")
	}

	cam := got["Webcam"]
	if len(cam) != 2 {
		t.Errorf("la webcam doit livrer video_device_id et last_video_device_id, reçu %v", cam)
	}
	// Résolution et cadence appartiennent à l'équipe : les relever ici les
	// figerait machine par machine.
	if _, ok := cam["resolution"]; ok {
		t.Error("resolution ne doit pas être traitée comme un réglage local")
	}
}

func TestApplyRéinjecteEtSignaleLesManquants(t *testing.T) {
	c := decodeFixture(t)

	overrides := map[string]map[string]any{
		"Webcam": {"video_device_id": "MA_CAMERA_A_MOI"},
	}
	unconfigured := Apply(c, overrides)

	var cam, mic Source
	for _, s := range c.Sources() {
		switch s.Name() {
		case "Webcam":
			cam = s
		case "Micro":
			mic = s
		}
	}

	if got := cam.Settings()["video_device_id"]; got != "MA_CAMERA_A_MOI" {
		t.Errorf("l'appareil local devait être réinjecté, obtenu %v", got)
	}
	// La clé absente de mes réglages désignerait la caméra du publieur : elle
	// doit disparaître plutôt que d'être conservée.
	if _, présent := cam.Settings()["last_video_device_id"]; présent {
		t.Error("last_video_device_id venait du publieur, il devait être retiré")
	}
	if got := cam.Settings()["resolution"]; got != "1920x1080" {
		t.Errorf("la résolution est partagée et devait survivre, obtenu %v", got)
	}
	if _, présent := mic.Settings()["device_id"]; présent {
		t.Error("aucun micro mémorisé ici : l'appareil du publieur devait être retiré")
	}

	// Micro et Écran n'ont pas de réglage mémorisé sur cette machine.
	if len(unconfigured) != 2 || unconfigured[0] != "Micro" || unconfigured[1] != "Écran" {
		t.Errorf("sources à configurer attendues [Micro Écran], obtenu %v", unconfigured)
	}
}

func TestApplyAprèsExtractEstStable(t *testing.T) {
	// Le cycle réel : on relève les appareils à la fermeture d'OBS, et on les
	// réinjecte à la réception suivante. Rien ne doit se perdre au passage.
	source := decodeFixture(t)
	relevé := Extract(source)

	reçue := decodeFixture(t)
	if manquants := Apply(reçue, relevé); len(manquants) != 0 {
		t.Fatalf("tout était mémorisé, rien ne devait manquer : %v", manquants)
	}

	avant, _ := source.Normalize()
	après, _ := reçue.Normalize()
	if string(avant) != string(après) {
		t.Error("un aller-retour relever/réinjecter doit redonner la collection d'origine")
	}
}

func TestPériphériqueParDéfautResteEnPlace(t *testing.T) {
	// Cas rencontré dans de vraies collections : une capture audio réglée sur
	// « default » vise le périphérique par défaut du système. La valeur est la
	// même partout, donc elle traverse le serveur telle quelle.
	const avecDéfaut = `{"sources":[
	  {"id":"wasapi_input_capture","name":"Capture audio (entrée)",
	   "settings":{"device_id":"default","use_device_timing":false}}
	]}`

	c, err := Decode([]byte(avecDéfaut))
	if err != nil {
		t.Fatal(err)
	}

	// Elle n'est pas relevée : il n'y a pas d'identifiant machine à mémoriser.
	if relevé := Extract(c); len(relevé) != 0 {
		t.Errorf("« default » n'est pas un identifiant matériel à mémoriser, relevé %v", relevé)
	}

	// Elle survit à une publication.
	StripLocal(c)
	if got := c.Sources()[0].Settings()["device_id"]; got != "default" {
		t.Errorf("« default » devait rester dans ce qui part sur la régie, obtenu %v", got)
	}

	// Et à une réception sur une machine qui n'a rien mémorisé.
	unconfigured := Apply(c, nil)
	if got := c.Sources()[0].Settings()["device_id"]; got != "default" {
		t.Errorf("« default » devait survivre à la réception, obtenu %v", got)
	}
	if len(unconfigured) != 0 {
		t.Errorf("une source sur le périphérique par défaut marche partout, elle ne doit pas être signalée : %v", unconfigured)
	}
}

func TestAllerRetourAvecPériphériqueParDéfaut(t *testing.T) {
	const src = `{"sources":[
	  {"id":"wasapi_input_capture","name":"Défaut","settings":{"device_id":"default"}},
	  {"id":"dshow_input","name":"Cam","settings":{"video_device_id":"MATERIEL_PRECIS"}}
	]}`

	départ, _ := Decode([]byte(src))
	attendu, _ := départ.Normalize()

	travail, _ := Decode([]byte(src))
	appareils := Extract(travail)
	StripLocal(travail)
	Apply(travail, appareils)

	obtenu, _ := travail.Normalize()
	if string(attendu) != string(obtenu) {
		t.Errorf("publier puis recevoir doit rendre la collection intacte :\n%s\n%s", attendu, obtenu)
	}
}

func TestMergeConserveLesSourcesAbsentes(t *testing.T) {
	ancien := map[string]map[string]any{
		"Webcam":      {"video_device_id": "A"},
		"Vieux micro": {"device_id": "B"},
	}
	nouveau := map[string]map[string]any{
		"Webcam": {"video_device_id": "C"},
	}
	got := Merge(ancien, nouveau)

	if got["Webcam"]["video_device_id"] != "C" {
		t.Error("le relevé le plus récent doit gagner")
	}
	if _, ok := got["Vieux micro"]; !ok {
		t.Error("une source absente de la collection courante doit garder son appareil mémorisé")
	}
}
