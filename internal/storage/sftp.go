package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sftpWriter struct {
	client *sftp.Client
	conn   *ssh.Client
}

func newSFTP(ctx context.Context, addr, user string, creds Credentials) (Writer, error) {
	auth, err := sftpAuth(creds)
	if err != nil {
		return nil, err
	}

	hostKey, err := newTOFU(creds.KnownHostsFile)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKey.check,
		Timeout:         30 * time.Second,
	}

	d := net.Dialer{Timeout: cfg.Timeout}
	rawConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connexion à %s impossible : %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(rawConn, addr, cfg)
	if err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("authentification SSH refusée par %s : %w", addr, err)
	}
	conn := ssh.NewClient(c, chans, reqs)

	client, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("le serveur %s accepte SSH mais pas SFTP : %w", addr, err)
	}
	return &sftpWriter{client: client, conn: conn}, nil
}

func sftpAuth(creds Credentials) ([]ssh.AuthMethod, error) {
	if creds.KeyFile != "" {
		pem, err := os.ReadFile(creds.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("clé privée illisible : %w", err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			if _, ok := err.(*ssh.PassphraseMissingError); ok {
				return nil, fmt.Errorf("la clé %s est protégée par une phrase secrète, que cet outil ne sait pas demander — utilisez une clé sans phrase ou un mot de passe", creds.KeyFile)
			}
			return nil, fmt.Errorf("clé privée inexploitable : %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	if creds.Password == "" {
		return nil, fmt.Errorf("ni mot de passe ni clé privée fournis pour SFTP")
	}
	return []ssh.AuthMethod{ssh.Password(creds.Password)}, nil
}

func (w *sftpWriter) Put(ctx context.Context, path string, r io.Reader) error {
	if err := w.Mkdir(ctx, parentDir(path)); err != nil {
		return err
	}
	f, err := w.client.Create(path)
	if err != nil {
		return fmt.Errorf("création de %s impossible : %w", path, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("envoi de %s interrompu : %w", path, err)
	}
	return f.Close()
}

func (w *sftpWriter) Mkdir(ctx context.Context, dir string) error {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return nil
	}
	if err := w.client.MkdirAll(dir); err != nil {
		return fmt.Errorf("création du dossier %s impossible : %w", dir, err)
	}
	return nil
}

func (w *sftpWriter) Move(ctx context.Context, from, to string) error {
	// Beaucoup de serveurs refusent de renommer sur une cible existante.
	_ = w.client.Remove(to)
	if err := w.client.Rename(from, to); err != nil {
		return fmt.Errorf("renommage de %s en %s impossible : %w", from, to, err)
	}
	return nil
}

func (w *sftpWriter) Close() error {
	err := w.client.Close()
	w.conn.Close()
	return err
}

// tofu applique le principe « confiance au premier contact » : l'empreinte du
// serveur est mémorisée la première fois, puis vérifiée à chaque connexion.
//
// C'est le compromis adapté ici. Accepter n'importe quelle clé exposerait les
// identifiants à une interception ; exiger un fichier known_hosts renseigné à
// la main serait hors de portée du public visé.
type tofu struct {
	path  string
	known map[string]string // hôte -> empreinte SHA256
}

func newTOFU(path string) (*tofu, error) {
	t := &tofu{path: path, known: map[string]string{}}
	if path == "" {
		return t, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return t, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &t.known); err != nil {
		return nil, fmt.Errorf("%s est illisible : %w", path, err)
	}
	return t, nil
}

func (t *tofu) check(hostname string, remote net.Addr, key ssh.PublicKey) error {
	got := ssh.FingerprintSHA256(key)
	want, seen := t.known[hostname]
	if seen {
		if want != got {
			return fmt.Errorf(
				"l'empreinte SSH de %s a changé (attendue %s, reçue %s) — connexion refusée ; "+
					"si le serveur a réellement été réinstallé, supprimez son entrée dans %s",
				hostname, want, got, t.path)
		}
		return nil
	}
	t.known[hostname] = got
	return t.save()
}

func (t *tofu) save() error {
	if t.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(t.known, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.path, append(data, '\n'), 0o600)
}
