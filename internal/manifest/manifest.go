// Package manifest décrit l'état publié de la régie.
//
// Le manifeste est la seule source de vérité : il énumère chaque fichier avec
// son empreinte. On ne liste jamais un dossier distant, parce qu'un hébergement
// HTTP statique ne sait pas le faire de façon fiable — et c'est ce qui rend
// l'outil indépendant des particularités de chaque hébergeur.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Name est le chemin du manifeste dans le dossier de régie.
const Name = "manifest.json"

// CollectionPath est l'emplacement de la collection de scènes dans le dossier
// de régie. Fixe côté dépôt ; c'est le nom local qui est configurable.
const CollectionPath = "scenes/collection.json"

type File struct {
	Path   string `json:"path"` // relatif au dossier de régie, séparateurs /
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Canvas voyage dans le manifeste plutôt que dans un fichier de profil : c'est
// le seul réglage de profil qui doit être commun à toute l'équipe, puisque des
// canvas différents décalent la position de toutes les sources.
type Canvas struct {
	BaseCX   int    `json:"base_cx"`
	BaseCY   int    `json:"base_cy"`
	OutputCX int    `json:"output_cx"`
	OutputCY int    `json:"output_cy"`
	FPS      string `json:"fps"` // "60", "30", "59.94"…
}

type Manifest struct {
	Version int       `json:"version"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Message string    `json:"message,omitempty"`
	Canvas  Canvas    `json:"canvas"`
	Files   []File    `json:"files"`
}

func Parse(data []byte) (*Manifest, error) {
	m := &Manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("manifeste illisible : %w", err)
	}
	if m.Version <= 0 {
		return nil, fmt.Errorf("manifeste sans numéro de version exploitable")
	}
	return m, nil
}

func (m *Manifest) Encode() ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Find renvoie l'entrée d'un chemin donné.
func (m *Manifest) Find(path string) (File, bool) {
	for _, f := range m.Files {
		if f.Path == path {
			return f, true
		}
	}
	return File{}, false
}

// Sum est l'empreinte utilisée partout dans l'outil : dans le manifeste pour
// décider quoi télécharger, et sur la forme normalisée de la collection pour
// détecter qu'un membre a retouché ses scènes.
func Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
