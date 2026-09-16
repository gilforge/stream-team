package obs

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Collection est une collection de scènes décodée sans perte.
//
// Le décodage passe par UseNumber : sans lui, tout nombre deviendrait un
// float64 et un identifiant de couleur ou un compteur 64 bits pourrait ressortir
// réécrit en notation scientifique. OBS relirait alors un fichier subtilement
// différent de celui qu'il avait écrit.
type Collection struct {
	Data map[string]any
}

func Decode(data []byte) (*Collection, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("collection de scènes illisible : %w", err)
	}
	return &Collection{Data: m}, nil
}

// Normalize rend une forme déterministe : même contenu, mêmes octets.
//
// C'est ce qui permet de comparer deux collections de façon fiable. OBS réécrit
// le fichier à sa manière en se fermant — ordre des clés et indentation lui
// appartiennent — donc une comparaison octet à octet signalerait une
// modification à chaque session, même sans qu'un membre ait touché à rien.
// L'empreinte de divergence porte toujours sur cette forme normalisée.
func (c *Collection) Normalize() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "    ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c.Data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Sources itère sur les entrées de la collection. Dans le format d'OBS, scènes
// et sources partagent le même tableau : une scène est une entrée dont le type
// vaut "scene", et ses éléments vivent dans ses réglages.
func (c *Collection) Sources() []Source {
	raw, ok := c.Data["sources"].([]any)
	if !ok {
		return nil
	}
	out := make([]Source, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Source{raw: m})
	}
	return out
}

// Source est une vue sur une entrée de la collection. Elle écrit dans la map
// d'origine : modifier une Source modifie la collection.
type Source struct {
	raw map[string]any
}

// Type est l'identifiant interne du type de source : dshow_input,
// monitor_capture, wasapi_input_capture… C'est lui qui décide si la source
// dépend du matériel local.
func (s Source) Type() string {
	v, _ := s.raw["id"].(string)
	return v
}

// Name est le libellé visible dans OBS. Il sert de clé pour mémoriser les
// réglages locaux : contrairement à l'uuid, il survit à une source recréée.
func (s Source) Name() string {
	v, _ := s.raw["name"].(string)
	return v
}

func (s Source) Settings() map[string]any {
	v, _ := s.raw["settings"].(map[string]any)
	return v
}

// EnsureSettings crée le bloc de réglages s'il manque, pour pouvoir y réinjecter
// un périphérique local.
func (s Source) EnsureSettings() map[string]any {
	if v, ok := s.raw["settings"].(map[string]any); ok {
		return v
	}
	m := map[string]any{}
	s.raw["settings"] = m
	return m
}
