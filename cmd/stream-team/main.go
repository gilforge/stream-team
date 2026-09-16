// Commande stream-team : le lanceur.
//
// Il synchronise la régie, ouvre OBS, sert le dock pendant la session, puis
// publie et s'efface. Il ne tourne jamais quand OBS ne tourne pas — rien au
// démarrage de Windows, aucun service, aucune trace sur la machine.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gilforge/stream-team/internal/config"
	"github.com/gilforge/stream-team/internal/dock"
	"github.com/gilforge/stream-team/internal/launcher"
	"github.com/gilforge/stream-team/internal/obs"
	"github.com/gilforge/stream-team/internal/pipeline"
	"github.com/gilforge/stream-team/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\n  Erreur : %v\n\n", err)
		pause()
		os.Exit(1)
	}
}

// checkOnly sert à éprouver la configuration sans ouvrir OBS : utile pour
// vérifier qu'une régie répond et que les scènes arrivent, avant de confier
// l'outil à toute une équipe.
var checkOnly = flag.Bool("check", false, "synchroniser puis s'arrêter, sans lancer OBS")

func run() error {
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dir, err := config.Dir()
	if err != nil {
		return err
	}

	cfg, configured, err := config.Load(dir)
	if err != nil {
		return err
	}
	if !configured {
		if err := setup(cfg); err != nil {
			return err
		}
		if err := cfg.Save(dir); err != nil {
			return err
		}
		fmt.Printf("\n  Configuration enregistrée dans %s\n", filepath.Join(dir, config.FileConfig))
		fmt.Printf("  Dans OBS : Docks → Dock de navigateur personnalisé → http://127.0.0.1:%d\n\n", cfg.DockPort)
	}

	pub, err := config.LoadPublisher(dir)
	if err != nil {
		return err
	}
	overrides, err := config.LoadOverrides(dir)
	if err != nil {
		return err
	}

	obsExe, err := launcher.Find(cfg.OBSPath)
	if err != nil {
		return err
	}
	paths, err := obs.FindPaths(obsExe)
	if err != nil {
		return err
	}
	reader, err := storage.NewHTTPReader(cfg.ReadURL)
	if err != nil {
		return err
	}

	engine := &pipeline.Engine{
		Dir:       dir,
		Cfg:       cfg,
		Pub:       pub,
		Paths:     paths,
		Reader:    reader,
		Overrides: overrides,
	}

	// 1. Réception, OBS étant encore fermé : c'est la seule fenêtre où écrire
	//    dans ses fichiers a un effet.
	//
	//    Un échec n'interrompt pas le lancement. Une régie injoignable un soir
	//    de stream ne doit jamais empêcher quelqu'un de diffuser : il part avec
	//    les scènes qu'il a déjà, ce qui est toujours mieux que pas d'OBS.
	report, err := engine.Receive(ctx, logStep)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  Mise à jour impossible : %v\n", err)
		fmt.Fprintf(os.Stderr, "  OBS démarre avec vos scènes actuelles.\n\n")
		report = &pipeline.Report{Version: cfg.AppliedVersion}
	}
	for _, name := range report.Unconfigured {
		fmt.Printf("  À configurer dans OBS : %s\n", name)
	}

	if *checkOnly {
		fmt.Printf("\n  Régie v%d", report.Version)
		if report.Author != "" {
			fmt.Printf(" publiée par %s", report.Author)
		}
		fmt.Printf("\n  Assets : %d téléchargé(s), %d déjà à jour\n", report.AssetsFetched, report.AssetsSkipped)
		if report.CanvasChanged {
			fmt.Println("  Canvas ajusté sur celui de l'équipe")
		}
		fmt.Println("\n  Vérification terminée, OBS n'a pas été lancé.")
		return nil
	}

	// 2. Le dock, servi en local pendant toute la session.
	srv := dock.New(engine, report)
	if err := srv.Start(cfg.DockPort); err != nil {
		fmt.Fprintf(os.Stderr, "  Dock indisponible : %v\n", err)
	}

	// 3. OBS, jusqu'à sa fermeture.
	logStep("Lancement d'OBS…")
	if err := launcher.Run(ctx, obsExe); err != nil {
		srv.Stop()
		return err
	}
	wanted, message := srv.PublishRequested()
	srv.Stop()

	// 4. OBS vient d'écrire ses fichiers : on relève les appareils de cette
	//    machine avant toute autre chose.
	if err := engine.CaptureDevices(); err != nil {
		fmt.Fprintf(os.Stderr, "  Relevé des périphériques impossible : %v\n", err)
	}

	// 5. Publication, si elle a été demandée et qu'il y a matière.
	if pub == nil {
		return nil
	}
	changed, err := engine.HasLocalChanges()
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if !wanted && !cfg.AutoPublish {
		fmt.Println("\n  Vos scènes ont changé mais n'ont pas été publiées.")
		fmt.Println("  Cochez « Publier en quittant » dans le dock avant de fermer OBS.")
		return nil
	}
	if _, err := engine.Publish(ctx, message, logStep); err != nil {
		return err
	}
	return nil
}

func logStep(msg string) {
	fmt.Printf("  %s\n", msg)
}

// setup est l'assistant de premier lancement. Une console pour l'instant : une
// vraie fenêtre viendra, mais elle ne change rien à ce qui est demandé.
func setup(cfg *config.Config) error {
	in := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("  Première configuration")
	fmt.Println("  ----------------------")
	fmt.Println()

	cfg.ReadURL = ask(in, "Adresse HTTP de la régie", "https://exemple.fr/streamteam/", "")
	if cfg.ReadURL == "" {
		return fmt.Errorf("aucune adresse saisie")
	}
	if !strings.HasSuffix(cfg.ReadURL, "/") {
		cfg.ReadURL += "/"
	}

	reader, err := storage.NewHTTPReader(cfg.ReadURL)
	if err != nil {
		return err
	}
	fmt.Println("  Vérification…")
	if err := reader.Probe(context.Background()); err != nil {
		return fmt.Errorf("adresse inutilisable : %w", err)
	}
	fmt.Println("  Régie joignable.")
	fmt.Println()

	defaultAssets := filepath.Join(os.Getenv("USERPROFILE"), "StreamAssets")
	cfg.AssetsDir = ask(in, "Dossier local des overlays", defaultAssets, defaultAssets)
	cfg.Collection = ask(in, "Nom de la collection de scènes dans OBS", "Équipe", "Équipe")
	cfg.Profile = ask(in, "Nom du profil OBS (vide = ne pas toucher au canvas)", "", "")

	return os.MkdirAll(cfg.AssetsDir, 0o755)
}

func ask(in *bufio.Scanner, label, placeholder, fallback string) string {
	if placeholder != "" {
		fmt.Printf("  %s\n  [%s] > ", label, placeholder)
	} else {
		fmt.Printf("  %s\n  > ", label)
	}
	if !in.Scan() {
		return fallback
	}
	answer := strings.TrimSpace(in.Text())
	if answer == "" {
		return fallback
	}
	return answer
}

func pause() {
	fmt.Print("  Appuyez sur Entrée pour fermer…")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
