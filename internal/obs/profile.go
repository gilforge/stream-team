package obs

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Canvas regroupe les seuls réglages de profil que l'agent touche.
//
// Le profil n'est jamais remplacé comme fichier : il porte l'encodeur, les
// périphériques audio, les chemins d'enregistrement et la clé de flux, c'est-à-
// dire précisément ce qui diffère d'une machine à l'autre. Mais la résolution
// de canvas doit être commune : si elle diffère, la position de toutes les
// sources de la collection partagée est décalée.
type Canvas struct {
	BaseCX   int
	BaseCY   int
	OutputCX int
	OutputCY int
	FPS      string
}

func (c Canvas) Valid() bool {
	return c.BaseCX > 0 && c.BaseCY > 0 && c.OutputCX > 0 && c.OutputCY > 0
}

func (c Canvas) String() string {
	return fmt.Sprintf("%dx%d → %dx%d @ %s", c.BaseCX, c.BaseCY, c.OutputCX, c.OutputCY, c.FPS)
}

// ReadCanvas lit la section [Video] du basic.ini d'un profil.
func ReadCanvas(profileDir string) (Canvas, error) {
	path := profileDir + string(os.PathSeparator) + "basic.ini"
	values, err := iniSection(path, "Video")
	if err != nil {
		return Canvas{}, err
	}
	c := Canvas{FPS: values["FPSCommon"]}
	c.BaseCX, _ = strconv.Atoi(values["BaseCX"])
	c.BaseCY, _ = strconv.Atoi(values["BaseCY"])
	c.OutputCX, _ = strconv.Atoi(values["OutputCX"])
	c.OutputCY, _ = strconv.Atoi(values["OutputCY"])
	if c.FPS == "" {
		c.FPS = "60"
	}
	return c, nil
}

// ApplyCanvas écrit les valeurs communes sans toucher au reste du fichier.
//
// La réécriture est ciblée, ligne par ligne : remplacer le basic.ini entier
// effacerait l'encodeur et la clé de flux du membre.
func ApplyCanvas(profileDir string, want Canvas) (bool, error) {
	if !want.Valid() {
		return false, nil
	}
	path := profileDir + string(os.PathSeparator) + "basic.ini"
	updates := map[string]string{
		"BaseCX":    strconv.Itoa(want.BaseCX),
		"BaseCY":    strconv.Itoa(want.BaseCY),
		"OutputCX":  strconv.Itoa(want.OutputCX),
		"OutputCY":  strconv.Itoa(want.OutputCY),
		"FPSType":   "0", // 0 = valeur commune, celle que règle FPSCommon
		"FPSCommon": want.FPS,
	}
	return iniSetSection(path, "Video", updates)
}

// iniValue lit une clé isolée.
func iniValue(path, section, key string) (string, error) {
	values, err := iniSection(path, section)
	if err != nil {
		return "", err
	}
	v, ok := values[key]
	if !ok {
		return "", fmt.Errorf("clé %s absente de [%s] dans %s", key, section, path)
	}
	return v, nil
}

func iniSection(path, section string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := map[string]string{}
	current := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = line[1 : len(line)-1]
			continue
		}
		if current != section {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			values[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return values, sc.Err()
}

// iniSetSection remplace les clés présentes, ajoute celles qui manquent à la fin
// de leur section, et crée la section si elle n'existe pas. Tout le reste du
// fichier est recopié à l'identique.
func iniSetSection(path, section string, updates map[string]string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	eol := "\n"
	if strings.Contains(string(raw), "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	remaining := make(map[string]string, len(updates))
	for k, v := range updates {
		remaining[k] = v
	}

	var out []string
	changed := false
	inSection := false
	sectionSeen := false
	endOfSection := -1

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inSection {
				endOfSection = len(out)
				inSection = false
			}
			if trimmed[1:len(trimmed)-1] == section {
				inSection = true
				sectionSeen = true
			}
			out = append(out, line)
			continue
		}
		if inSection {
			if k, old, ok := strings.Cut(trimmed, "="); ok {
				key := strings.TrimSpace(k)
				if want, found := remaining[key]; found {
					delete(remaining, key)
					if strings.TrimSpace(old) != want {
						changed = true
					}
					out = append(out, key+"="+want)
					continue
				}
			}
		}
		out = append(out, line)
	}
	if inSection {
		endOfSection = len(out)
	}

	if len(remaining) > 0 {
		changed = true
		added := make([]string, 0, len(remaining))
		for _, k := range sortedKeys(remaining) {
			added = append(added, k+"="+remaining[k])
		}
		switch {
		case !sectionSeen:
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			out = append(out, "["+section+"]")
			out = append(out, added...)
		case endOfSection >= 0:
			tail := append([]string{}, out[endOfSection:]...)
			out = append(out[:endOfSection], added...)
			out = append(out, tail...)
		default:
			out = append(out, added...)
		}
	}

	if !changed {
		return false, nil
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(out, eol)), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
