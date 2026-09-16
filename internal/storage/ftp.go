package storage

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

type tlsMode int

const (
	tlsNone     tlsMode = iota // ftp://  — tout circule en clair, identifiants compris
	tlsExplicit                // ftps:// — AUTH TLS, ce que proposent la plupart des hébergeurs
)

type ftpWriter struct {
	conn *ftp.ServerConn
}

func newFTP(ctx context.Context, addr, user, password string, mode tlsMode) (Writer, error) {
	opts := []ftp.DialOption{
		ftp.DialWithContext(ctx),
		ftp.DialWithTimeout(30 * time.Second),
	}
	if mode == tlsExplicit {
		host := addr
		if i := strings.LastIndex(addr, ":"); i > 0 {
			host = addr[:i]
		}
		// Vérification du certificat laissée active : un hébergement qui
		// propose FTPS a un certificat valide, et le désactiver retirerait
		// tout l'intérêt du chiffrement.
		opts = append(opts, ftp.DialWithExplicitTLS(&tls.Config{ServerName: host}))
	}

	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("connexion à %s impossible : %w", addr, err)
	}
	if err := conn.Login(user, password); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("identifiants refusés par %s : %w", addr, err)
	}
	return &ftpWriter{conn: conn}, nil
}

func (w *ftpWriter) Put(ctx context.Context, path string, r io.Reader) error {
	if err := w.Mkdir(ctx, parentDir(path)); err != nil {
		return err
	}
	if err := w.conn.Stor(path, r); err != nil {
		return fmt.Errorf("envoi de %s impossible : %w", path, err)
	}
	return nil
}

// Mkdir crée toute la chaîne de dossiers. FTP n'a pas d'équivalent de mkdir -p :
// on remonte segment par segment, et l'échec d'un segment existant est normal.
func (w *ftpWriter) Mkdir(ctx context.Context, dir string) error {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return nil
	}
	var built string
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" {
			continue
		}
		if built == "" {
			built = seg
		} else {
			built += "/" + seg
		}
		// L'erreur est ignorée volontairement : le serveur répond 550 quand le
		// dossier existe déjà, ce qui est le cas courant. Un vrai problème de
		// droits ressortira au Stor suivant, avec un message plus parlant.
		_ = w.conn.MakeDir(built)
	}
	return nil
}

func (w *ftpWriter) Move(ctx context.Context, from, to string) error {
	if err := w.conn.Rename(from, to); err != nil {
		return fmt.Errorf("renommage de %s en %s impossible : %w", from, to, err)
	}
	return nil
}

func (w *ftpWriter) Close() error {
	return w.conn.Quit()
}

func parentDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return ""
	}
	return p[:i]
}
