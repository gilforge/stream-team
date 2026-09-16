package storage

import (
	"context"
	"errors"
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

// tentatives et attenteBase pilotent la reprise sur limitation de débit. Les
// hébergements mutualisés protègent leurs serveurs contre les rafales de
// requêtes, et un agent qui synchronise plusieurs fichiers d'affilée peut
// franchir le seuil sans rien faire d'anormal.
const (
	tentatives  = 3
	attenteBase = 2 * time.Second
)

func (r *HTTPReader) do(ctx context.Context, p string, noCache bool) (*http.Response, error) {
	var dernière error
	for essai := 0; essai < tentatives; essai++ {
		resp, err := r.tenter(ctx, p, noCache)
		if err == nil {
			return resp, nil
		}
		dernière = err

		attente, limité := délaiAvantReprise(err, essai)
		if !limité {
			return nil, err
		}
		select {
		case <-time.After(attente):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, dernière
}

// erreurLimitée signale que le serveur a refusé temporairement, en disant
// éventuellement combien de temps attendre.
type erreurLimitée struct {
	statut  string
	chemin  string
	attente time.Duration
}

func (e *erreurLimitée) Error() string {
	if e.attente > 0 {
		return fmt.Sprintf("la régie limite les requêtes (%s) — réessayez dans %s", e.statut, e.attente)
	}
	return fmt.Sprintf("la régie limite les requêtes (%s) pour %s — patientez une minute puis réessayez", e.statut, e.chemin)
}

func délaiAvantReprise(err error, essai int) (time.Duration, bool) {
	var limité *erreurLimitée
	if !errors.As(err, &limité) {
		return 0, false
	}
	if limité.attente > 0 {
		return limité.attente, true
	}
	return attenteBase << essai, true // 2 s, puis 4 s
}

func (r *HTTPReader) tenter(ctx context.Context, p string, noCache bool) (*http.Response, error) {
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
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, fmt.Errorf("%s est introuvable sur la régie : %w", p, ErrNotFound)
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			return nil, &erreurLimitée{
				statut:  resp.Status,
				chemin:  p,
				attente: retryAfter(resp.Header.Get("Retry-After")),
			}
		}
		return nil, fmt.Errorf("la régie répond %s pour %s", resp.Status, p)
	}
	return resp, nil
}

// retryAfter lit l'en-tête du même nom, quand le serveur prend la peine de dire
// combien de temps patienter. On plafonne : au-delà d'une minute, mieux vaut
// rendre la main que faire attendre quelqu'un devant un écran.
func retryAfter(v string) time.Duration {
	secondes, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secondes <= 0 {
		return 0
	}
	if secondes > 60 {
		return 0
	}
	return time.Duration(secondes) * time.Second
}

// ProbeResult décrit ce qu'on a pu apprendre d'une adresse. Une régie vide est
// un état parfaitement normal — c'est celui d'un dossier qu'on vient de créer —
// donc l'absence de manifeste n'est pas une erreur, seulement une information.
type ProbeResult struct {
	DirExists     bool // la racine du dossier ne renvoie pas 404
	ManifestFound bool
}

// Probe valide une adresse au moment où l'utilisateur la saisit.
//
// Elle ne renvoie d'erreur que pour ce qu'il peut réellement corriger sur-le-
// champ : un domaine injoignable, un certificat invalide, une adresse qui rend
// une page web au lieu d'un manifeste. Le reste est rapporté tel quel, à charge
// pour l'appelant d'en tirer le bon message.
func (r *HTTPReader) Probe(ctx context.Context, manifestPath string) (ProbeResult, error) {
	var out ProbeResult

	// La racine du dossier : un 404 ici signale presque toujours un chemin mal
	// recopié, tandis qu'un 403 ou un listing signifie que le dossier est là.
	if _, err := r.Get(ctx, "."); err == nil {
		out.DirExists = true
	} else if !errors.Is(err, ErrNotFound) {
		if estRéseau(err) {
			return out, err
		}
		out.DirExists = true // 403 sur un dossier sans index : il existe bel et bien
	}

	data, err := r.GetNoCache(ctx, manifestPath)
	switch {
	case errors.Is(err, ErrNotFound):
		return out, nil
	case err != nil:
		return out, err
	}

	if len(data) > 0 && data[0] == '<' {
		return out, fmt.Errorf("l'adresse rend une page web au lieu d'un manifeste — pointe-t-elle bien sur le dossier de la régie ?")
	}
	out.DirExists, out.ManifestFound = true, true
	return out, nil
}

// estRéseau distingue « le serveur a répondu quelque chose » de « on n'a pas pu
// lui parler ».
func estRéseau(err error) bool {
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}
