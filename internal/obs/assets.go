package obs

import (
	"encoding/json"
	"sort"
	"strings"
)

// AssetsToken remplace le dossier d'assets dans la collection publiée.
//
// Sans cela, une collection porterait « C:\Users\marc\Overlays\alerte.png » et
// n'afficherait rien chez quelqu'un dont le dossier s'appelle autrement. Le
// jeton est réécrit vers le dossier local à chaque réception, et retokenisé à
// chaque publication.
const AssetsToken = "$ASSETS$"

// Tokenize remplace le dossier d'assets local par le jeton, avant publication.
// Renvoie le nombre de chemins réécrits.
func Tokenize(c *Collection, assetsDir string) int {
	if assetsDir == "" {
		return 0
	}
	prefix := normalizePath(assetsDir)
	count := 0
	walkStrings(c.Data, func(s string) (string, bool) {
		cand := normalizePath(s)
		if !strings.HasPrefix(strings.ToLower(cand), strings.ToLower(prefix)) {
			return s, false
		}
		count++
		return AssetsToken + cand[len(prefix):], true
	})
	return count
}

// Expand fait l'inverse au moment de la réception : le jeton redevient le
// dossier de cette machine.
func Expand(c *Collection, assetsDir string) int {
	prefix := normalizePath(assetsDir)
	count := 0
	walkStrings(c.Data, func(s string) (string, bool) {
		if !strings.Contains(s, AssetsToken) {
			return s, false
		}
		count++
		return strings.ReplaceAll(s, AssetsToken, prefix), true
	})
	return count
}

// ForeignPaths liste les chemins absolus qui échappent au dossier d'assets.
//
// Ce sont les pièges d'une première publication : une image laissée sur le
// bureau ou une vidéo restée dans un dossier de téléchargement part telle
// quelle, avec son chemin complet, et n'existe chez personne d'autre. Le membre
// qui reçoit voit une source vide sans comprendre pourquoi.
//
// On les signale plutôt que de les corriger : déplacer les fichiers de
// quelqu'un serait bien plus surprenant que de l'avertir.
func ForeignPaths(c *Collection, assetsDir string) []string {
	prefix := strings.ToLower(normalizePath(assetsDir))
	vus := map[string]bool{}
	var out []string

	walkStrings(c.Data, func(s string) (string, bool) {
		if !ressembleÀUnChemin(s) {
			return s, false
		}
		cand := strings.ToLower(normalizePath(s))
		if prefix != "" && strings.HasPrefix(cand, prefix) {
			return s, false
		}
		if !vus[cand] {
			vus[cand] = true
			out = append(out, s)
		}
		return s, false
	})
	sort.Strings(out)
	return out
}

// ressembleÀUnChemin reconnaît un chemin absolu local sans se laisser prendre
// par une URL : une source navigateur pointant sur le web est parfaitement
// partageable.
func ressembleÀUnChemin(s string) bool {
	if s == "" || strings.Contains(s, AssetsToken) {
		return false
	}
	if strings.Contains(s, "://") {
		return false // http://, https://, srt://…
	}
	p := normalizePath(s)
	// Lettre de lecteur Windows, ou chemin Unix absolu.
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		return true
	}
	return strings.HasPrefix(p, "/") && strings.Contains(p, ".")
}

// MissingAssets liste les chemins tokenisés d'une collection, pour vérifier que
// chaque fichier référencé est bien arrivé.
func MissingAssets(c *Collection) []string {
	var out []string
	walkStrings(c.Data, func(s string) (string, bool) {
		if strings.Contains(s, AssetsToken) {
			out = append(out, s)
		}
		return s, false
	})
	return out
}

// walkStrings parcourt toute la structure et laisse la fonction réécrire chaque
// chaîne. Un parcours exhaustif plutôt qu'une liste de clés connues : les
// chemins se cachent dans les réglages de source, mais aussi dans les filtres,
// les transitions à vidéo et les polices.
func walkStrings(node any, fn func(string) (string, bool)) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if s, ok := child.(string); ok {
				if replaced, changed := fn(s); changed {
					v[k] = replaced
				}
				continue
			}
			walkStrings(child, fn)
		}
	case []any:
		for i, child := range v {
			if s, ok := child.(string); ok {
				if replaced, changed := fn(s); changed {
					v[i] = replaced
				}
				continue
			}
			walkStrings(child, fn)
		}
	}
}

// normalizePath uniformise les séparateurs. OBS écrit des barres obliques dans
// son JSON même sous Windows, mais un chemin saisi par l'utilisateur arrive
// avec des antislashs.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.TrimSuffix(p, "/")
}

// DecodeRaw expose le JSON brut d'une collection, pour les tests et le
// diagnostic.
func (c *Collection) DecodeRaw() (string, error) {
	data, err := json.MarshalIndent(c.Data, "", "  ")
	return string(data), err
}
