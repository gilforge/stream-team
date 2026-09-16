package obs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// LoadCollection lit une collection depuis le disque.
func LoadCollection(path string) (*Collection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// SaveCollection écrit une collection sous sa forme normalisée.
//
// L'écriture passe par un fichier temporaire : une interruption ne doit pas
// laisser une collection tronquée, qui ferait démarrer OBS sur des scènes vides.
func SaveCollection(path string, c *Collection) error {
	data, err := c.Normalize()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Backup copie la collection actuelle avant qu'une réception ne l'écrase, et ne
// garde que les plus récentes.
func Backup(collectionPath, backupDir string, keep int) error {
	data, err := os.ReadFile(collectionPath)
	if os.IsNotExist(err) {
		return nil // rien à sauvegarder au tout premier lancement
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.json",
		time.Now().Format("2006-01-02-150405"),
		trimExt(filepath.Base(collectionPath)))
	if err := os.WriteFile(filepath.Join(backupDir, name), data, 0o644); err != nil {
		return err
	}
	return pruneBackups(backupDir, keep)
}

func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return nil
	}
	sort.Strings(names) // l'horodatage en tête rend l'ordre alphabétique chronologique
	for _, name := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}

func trimExt(name string) string {
	return name[:len(name)-len(filepath.Ext(name))]
}
