package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeRégieVide(t *testing.T) {
	// Un dossier créé chez l'hébergeur mais pas encore alimenté : la racine
	// répond, le manifeste non. C'est un état normal, pas une erreur.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("Index of /twitch"))
	}))
	defer srv.Close()

	reader, _ := NewHTTPReader(srv.URL + "/")
	res, err := reader.Probe(context.Background(), "manifest.json")
	if err != nil {
		t.Fatalf("une régie vide ne doit pas produire d'erreur : %v", err)
	}
	if !res.DirExists {
		t.Error("le dossier répond, il devait être reconnu comme existant")
	}
	if res.ManifestFound {
		t.Error("il n'y a pas de manifeste sur cette régie")
	}
}

func TestProbeDossierInexistant(t *testing.T) {
	// Chemin mal recopié : tout est en 404. On le rapporte sans bloquer, car
	// le dossier peut aussi être créé par la première publication FTP.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	reader, _ := NewHTTPReader(srv.URL + "/absent/")
	res, err := reader.Probe(context.Background(), "manifest.json")
	if err != nil {
		t.Fatalf("un 404 n'est pas une panne : %v", err)
	}
	if res.DirExists || res.ManifestFound {
		t.Errorf("rien ne répond ici, obtenu %+v", res)
	}
}

func TestProbeRégieAlimentée(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":3}`))
	}))
	defer srv.Close()

	reader, _ := NewHTTPReader(srv.URL + "/")
	res, err := reader.Probe(context.Background(), "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if !res.DirExists || !res.ManifestFound {
		t.Errorf("la régie est alimentée, obtenu %+v", res)
	}
}

func TestProbeAdresseQuiRendUnePageWeb(t *testing.T) {
	// Cas courant : l'adresse pointe sur le site plutôt que sur le dossier de
	// régie, et l'hébergeur sert sa page d'accueil en 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!DOCTYPE html><html><body>Accueil</body></html>"))
	}))
	defer srv.Close()

	reader, _ := NewHTTPReader(srv.URL + "/")
	if _, err := reader.Probe(context.Background(), "manifest.json"); err == nil {
		t.Fatal("une page web à la place du manifeste doit être signalée")
	}
}

func TestProbeServeurInjoignable(t *testing.T) {
	reader, _ := NewHTTPReader("http://127.0.0.1:1/")
	if _, err := reader.Probe(context.Background(), "manifest.json"); err == nil {
		t.Fatal("un serveur injoignable doit produire une erreur")
	}
}

func TestErrNotFoundEstReconnaissable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	reader, _ := NewHTTPReader(srv.URL + "/")
	_, err := reader.Get(context.Background(), "manifest.json")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("un 404 doit être identifiable par errors.Is, obtenu %v", err)
	}
}
