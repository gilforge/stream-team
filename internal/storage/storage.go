// Package storage sépare la lecture de l'écriture, parce qu'elles ne concernent
// pas les mêmes personnes.
//
// Toute l'équipe lit, en HTTP, sans identifiants : coller une adresse suffit.
// Une seule personne écrit, par FTP, FTPS ou SFTP — les trois protocoles que
// propose n'importe quel hébergement web mutualisé. Aucun des deux rôles ne
// demande d'administrer un serveur, ce qui est la condition pour que l'outil
// serve à d'autres équipes que la nôtre.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// ErrNotFound distingue « ce fichier n'existe pas encore » d'une panne ou d'une
// adresse erronée. La différence compte à la première publication : une régie
// vide est un état normal, pas une erreur.
var ErrNotFound = errors.New("fichier absent de la régie")

// Reader est présent sur tous les postes.
type Reader interface {
	// Get rapatrie un petit fichier en mémoire (manifeste, collection).
	Get(ctx context.Context, path string) ([]byte, error)
	// GetNoCache force un aller-retour réseau. Réservé au manifeste : un
	// manifeste servi depuis un cache ferait croire à un membre qu'il est à
	// jour alors qu'il ne l'est pas.
	GetNoCache(ctx context.Context, path string) ([]byte, error)
	// Download écrit un fichier volumineux directement sur le disque, sans le
	// charger en mémoire — les assets peuvent peser plusieurs gigaoctets.
	Download(ctx context.Context, path, destPath string) error
}

// Writer n'est instancié que chez le publieur.
type Writer interface {
	Put(ctx context.Context, path string, r io.Reader) error
	Mkdir(ctx context.Context, path string) error
	Move(ctx context.Context, from, to string) error
	Close() error
}

// Credentials reprend ce que porte publisher.json, plus l'emplacement du
// fichier où SFTP mémorise l'empreinte des serveurs déjà rencontrés.
type Credentials struct {
	Password       string
	KeyFile        string
	KnownHostsFile string
}

// NewWriter choisit l'implémentation d'après le schéma de l'URL :
//
//	ftp://user@hote:21/www/regie   texte en clair, identifiants compris
//	ftps://user@hote:21/www/regie  FTP avec AUTH TLS (chiffrement explicite)
//	sftp://user@hote:22/www/regie  transfert sur SSH
//
// Le mot de passe ne figure jamais dans l'URL : il reste dans un champ à part
// pour ne pas se retrouver recopié dans un message d'erreur.
func NewWriter(ctx context.Context, rawURL string, creds Credentials) (Writer, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("adresse d'écriture illisible : %w", err)
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, "", fmt.Errorf("l'adresse d'écriture doit porter un nom d'utilisateur, par exemple ftps://marc@%s/...", u.Host)
	}
	user := u.User.Username()
	base := strings.TrimSuffix(u.Path, "/")

	switch strings.ToLower(u.Scheme) {
	case "ftp":
		w, err := newFTP(ctx, hostPort(u.Host, "21"), user, creds.Password, tlsNone)
		return w, base, err
	case "ftps":
		w, err := newFTP(ctx, hostPort(u.Host, "21"), user, creds.Password, tlsExplicit)
		return w, base, err
	case "sftp":
		w, err := newSFTP(ctx, hostPort(u.Host, "22"), user, creds)
		return w, base, err
	default:
		return nil, "", fmt.Errorf("protocole d'écriture %q inconnu — utilisez ftp://, ftps:// ou sftp://", u.Scheme)
	}
}

func hostPort(host, defaultPort string) string {
	if strings.Contains(host, ":") {
		return host
	}
	return host + ":" + defaultPort
}
