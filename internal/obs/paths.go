// Package obs manipule les fichiers d'OBS Studio pendant qu'il est fermé.
//
// C'est la règle qui tient tout l'outil : OBS charge ses scènes en mémoire au
// démarrage et ne réécrit ses fichiers qu'à la fermeture. Écrire pendant qu'il
// tourne revient à voir son travail écrasé à la sortie. L'agent n'agit donc
// qu'avant le lancement et après la fermeture.
package obs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths localise la configuration d'OBS sur cette machine.
type Paths struct {
	Root     string // .../obs-studio
	Profiles string // .../basic/profiles
	Scenes   string // .../basic/scenes
	UserINI  string // global.ini ou user.ini selon la version
}

// FindPaths gère les deux installations possibles. En mode portable — fréquent
// chez les streamers qui trimballent OBS sur un disque externe — la
// configuration vit à côté de l'exécutable et non dans %APPDATA%.
func FindPaths(obsExe string) (*Paths, error) {
	var root string

	if obsExe != "" {
		// bin/64bit/obs64.exe -> racine de l'installation
		instDir := filepath.Dir(filepath.Dir(filepath.Dir(obsExe)))
		for _, marker := range []string{"portable_mode.txt", "portable_mode"} {
			if _, err := os.Stat(filepath.Join(instDir, marker)); err == nil {
				root = filepath.Join(instDir, "config", "obs-studio")
				break
			}
		}
	}

	if root == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return nil, fmt.Errorf("variable APPDATA introuvable : impossible de localiser la configuration d'OBS")
		}
		root = filepath.Join(appData, "obs-studio")
	}

	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("configuration d'OBS introuvable dans %s — lancez OBS une fois avant d'utiliser cet outil", root)
	}

	p := &Paths{
		Root:     root,
		Profiles: filepath.Join(root, "basic", "profiles"),
		Scenes:   filepath.Join(root, "basic", "scenes"),
	}
	// OBS 30 a déplacé une partie de global.ini vers user.ini. On prend le
	// premier présent plutôt que de supposer une version.
	for _, name := range []string{"user.ini", "global.ini"} {
		candidate := filepath.Join(root, name)
		if _, err := os.Stat(candidate); err == nil {
			p.UserINI = candidate
			break
		}
	}
	return p, nil
}

// CollectionFile retrouve le fichier d'une collection par son nom affiché.
//
// OBS dérive le nom de fichier du nom de la collection en remplaçant les
// caractères qu'un système de fichiers refuse, et les règles ont changé selon
// les versions. Plutôt que de rejouer cette transformation, on ouvre les
// fichiers et on lit leur champ "name" : c'est la seule méthode qui reste juste
// quelle que soit la version d'OBS.
func (p *Paths) CollectionFile(displayName string) (string, error) {
	entries, err := os.ReadDir(p.Scenes)
	if err != nil {
		return "", fmt.Errorf("dossier des collections illisible : %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		full := filepath.Join(p.Scenes, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var head struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &head) == nil && head.Name == displayName {
			return full, nil
		}
	}
	return "", fmt.Errorf("aucune collection nommée %q dans OBS — créez-la une fois dans OBS, ou corrigez le nom dans la configuration", displayName)
}

// NewCollectionFile calcule le chemin d'une collection encore inexistante.
func (p *Paths) NewCollectionFile(displayName string) string {
	return filepath.Join(p.Scenes, sanitize(displayName)+".json")
}

// ProfileDir renvoie le dossier d'un profil, repéré par le nom stocké dans son
// basic.ini.
func (p *Paths) ProfileDir(displayName string) (string, error) {
	entries, err := os.ReadDir(p.Profiles)
	if err != nil {
		return "", fmt.Errorf("dossier des profils illisible : %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		iniPath := filepath.Join(p.Profiles, e.Name(), "basic.ini")
		name, err := iniValue(iniPath, "General", "Name")
		if err == nil && name == displayName {
			return filepath.Join(p.Profiles, e.Name()), nil
		}
	}
	return "", fmt.Errorf("aucun profil nommé %q dans OBS", displayName)
}

// sanitize reproduit approximativement la transformation d'OBS. Elle ne sert
// qu'à créer un fichier neuf, jamais à en retrouver un existant.
func sanitize(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		out = "collection"
	}
	return out
}
