// Package config gère les trois fichiers que l'agent pose à côté de son exécutable.
//
//	config.json           écrit par l'agent, présent chez tout le monde
//	publisher.json        posé à la main, présent chez le seul publieur
//	local-overrides.json  écrit par l'agent, propre à chaque machine
//
// C'est la présence de publisher.json, et rien d'autre, qui fait d'un poste un
// poste publieur. Le binaire est identique pour toute l'équipe.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	FileConfig    = "config.json"
	FilePublisher = "publisher.json"
	FileOverrides = "local-overrides.json"
	DirBackups    = "backups"
)

// Config contient ce que l'utilisateur saisit au premier lancement, plus l'état
// de la dernière synchronisation réussie.
type Config struct {
	// ReadURL est l'adresse HTTP du dossier de régie, barre oblique finale
	// comprise. C'est la seule chose qu'un membre ordinaire a à fournir.
	ReadURL string `json:"read_url"`

	// AssetsDir est le dossier local où atterrissent overlays et vidéos. Les
	// chemins de la collection partagée y sont réécrits à chaque réception.
	AssetsDir string `json:"assets_dir"`

	// Collection et Profile nomment ce que l'agent pilote dans OBS. Jamais de
	// valeur en dur : chaque équipe a ses noms.
	Collection string `json:"collection"`
	Profile    string `json:"profile"`

	// OBSPath force l'emplacement de obs64.exe quand la détection échoue.
	OBSPath string `json:"obs_path,omitempty"`

	// DockPort est le port d'écoute local du dock. Fixe, parce que l'adresse
	// est collée une fois dans OBS et ne doit plus changer.
	DockPort int `json:"dock_port"`

	// AutoPublish déclenche la publication à la fermeture d'OBS sans demander,
	// dès qu'une divergence locale est détectée.
	AutoPublish bool `json:"auto_publish"`

	// AppliedVersion et AppliedHash décrivent l'état appliqué à la dernière
	// réception. AppliedHash porte sur la forme normalisée de la collection :
	// OBS réécrit le fichier à sa façon en se fermant, donc une comparaison
	// octet à octet signalerait une divergence à chaque session.
	AppliedVersion int    `json:"applied_version"`
	AppliedHash    string `json:"applied_hash"`
}

// Publisher porte les identifiants d'écriture. Absent chez la quasi-totalité de
// l'équipe.
type Publisher struct {
	// WriteURL vaut ftp://, ftps:// ou sftp://, avec l'utilisateur et le
	// chemin du dossier : sftp://marc@exemple.fr:22/www/streamteam
	WriteURL string `json:"write_url"`

	// Password reste hors de WriteURL pour ne pas se retrouver dans un message
	// d'erreur ou un journal.
	Password string `json:"password,omitempty"`

	// KeyFile désigne une clé privée OpenSSH (SFTP uniquement). Prioritaire sur
	// Password quand les deux sont renseignés.
	KeyFile string `json:"key_file,omitempty"`

	// Author apparaît dans le manifeste pour que l'équipe sache qui a publié.
	Author string `json:"author"`
}

// Overrides mémorise, par nom de source, les réglages qui appartiennent à cette
// machine et qu'aucune réception ne doit écraser.
type Overrides struct {
	Sources map[string]map[string]any `json:"sources"`
}

// Dir renvoie le dossier de l'exécutable, qui sert de racine à tous les
// fichiers de l'agent. L'outil ne dépose jamais rien ailleurs.
func Dir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("emplacement de l'exécutable introuvable : %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return filepath.Dir(resolved), nil
}

// Load lit config.json. Un fichier absent n'est pas une erreur : il signifie
// premier lancement, et l'appelant ouvre l'assistant de configuration.
func Load(dir string) (*Config, bool, error) {
	c := &Config{DockPort: 47838}
	err := readJSON(filepath.Join(dir, FileConfig), c)
	if os.IsNotExist(err) {
		return c, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if c.DockPort == 0 {
		c.DockPort = 47838
	}
	c.ReadURL = withTrailingSlash(c.ReadURL)
	return c, true, nil
}

func (c *Config) Save(dir string) error {
	return writeJSON(filepath.Join(dir, FileConfig), c)
}

// LoadPublisher renvoie nil sans erreur quand le fichier n'existe pas : ce
// poste est alors en lecture seule, et le dock n'affichera aucun bouton de
// publication.
func LoadPublisher(dir string) (*Publisher, error) {
	p := &Publisher{}
	err := readJSON(filepath.Join(dir, FilePublisher), p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.WriteURL == "" {
		return nil, fmt.Errorf("%s ne contient pas de write_url", FilePublisher)
	}
	if p.Author == "" {
		p.Author = "inconnu"
	}
	return p, nil
}

func LoadOverrides(dir string) (*Overrides, error) {
	o := &Overrides{Sources: map[string]map[string]any{}}
	err := readJSON(filepath.Join(dir, FileOverrides), o)
	if os.IsNotExist(err) {
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	if o.Sources == nil {
		o.Sources = map[string]map[string]any{}
	}
	return o, nil
}

func (o *Overrides) Save(dir string) error {
	return writeJSON(filepath.Join(dir, FileOverrides), o)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s est illisible : %w", filepath.Base(path), err)
	}
	return nil
}

// writeJSON écrit par fichier temporaire puis renommage : une coupure de
// courant en pleine écriture ne doit pas laisser un config.json tronqué, qui
// obligerait l'utilisateur à tout resaisir.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func withTrailingSlash(u string) string {
	if u == "" || strings.HasSuffix(u, "/") {
		return u
	}
	return u + "/"
}
