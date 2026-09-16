package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HTTPReader lit un dossier de régie servi par n'importe quel hébergement web.
// Il n'utilise que GET : pas de listage de dossier, pas de méthode exotique,
// donc rien qui puisse manquer sur un hébergement mutualisé.
type HTTPReader struct {
	base   *url.URL
	client *http.Client
}

func NewHTTPReader(baseURL string) (*HTTPReader, error) {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("adresse de régie illisible : %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("l'adresse de régie doit commencer par http:// ou https:// (reçu %q)", u.Scheme)
	}
	return &HTTPReader{
		base:   u,
		client: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (r *HTTPReader) Get(ctx context.Context, p string) ([]byte, error) {
	resp, err := r.do(ctx, p, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (r *HTTPReader) GetNoCache(ctx context.Context, p string) ([]byte, error) {
	resp, err := r.do(ctx, p, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (r *HTTPReader) Download(ctx context.Context, p, destPath string) error {
	resp, err := r.do(ctx, p, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	// Écriture en fichier temporaire : une coupure réseau au milieu d'un
	// téléchargement ne doit pas laisser un asset tronqué que l'empreinte
	// signalerait comme valide au prochain lancement.
	tmp := destPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destPath)
}

func (r *HTTPReader) do(ctx context.Context, p string, noCache bool) (*http.Response, error) {
	ref := &url.URL{Path: strings.TrimPrefix(path.Clean(p), "/")}
	full := r.base.ResolveReference(ref)

	if noCache {
		q := full.Query()
		q.Set("_", strconv.FormatInt(time.Now().UnixNano(), 36))
		full.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full.String(), nil)
	if err != nil {
		return nil, err
	}
	if noCache {
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Pragma", "no-cache")
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("téléchargement de %s impossible : %w", p, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%s est introuvable sur la régie (404) — vérifiez l'adresse", p)
		}
		return nil, fmt.Errorf("la régie répond %s pour %s", resp.Status, p)
	}
	return resp, nil
}

// Probe valide une adresse au moment où l'utilisateur la saisit, et distingue
// les erreurs qu'il peut réellement corriger.
func (r *HTTPReader) Probe(ctx context.Context) error {
	data, err := r.GetNoCache(ctx, "manifest.json")
	if err != nil {
		return err
	}
	if len(data) > 0 && data[0] == '<' {
		return fmt.Errorf("l'adresse renvoie une page web, pas un manifeste — pointe-t-elle bien sur le dossier de la régie ?")
	}
	return nil
}
