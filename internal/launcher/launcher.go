// Package launcher démarre OBS et attend qu'il se ferme.
//
// C'est ce qui permet à l'agent de n'être qu'un lanceur : il vit le temps d'une
// session, puis disparaît. Rien au démarrage de Windows, aucun service, aucune
// trace résiduelle sur la machine d'un membre de l'équipe.
package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Find localise l'exécutable d'OBS. Un chemin explicite venu de la
// configuration l'emporte toujours.
func Find(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("le chemin d'OBS indiqué dans la configuration est introuvable : %s", configured)
		}
		return configured, nil
	}
	for _, candidate := range defaultPaths() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("OBS introuvable — indiquez son emplacement dans la configuration (obs_path)")
}

func defaultPaths() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
			if base := os.Getenv(env); base != "" {
				out = append(out,
					filepath.Join(base, "obs-studio", "bin", "64bit", "obs64.exe"))
			}
		}
		return out
	case "darwin":
		return []string{"/Applications/OBS.app/Contents/MacOS/OBS"}
	default:
		return []string{"/usr/bin/obs", "/usr/local/bin/obs", "/var/lib/flatpak/exports/bin/com.obsproject.Studio"}
	}
}

// Run lance OBS et bloque jusqu'à sa fermeture.
//
// Le répertoire de travail est celui de l'exécutable : lancé depuis ailleurs,
// OBS ne trouve ni ses bibliothèques ni ses traductions et s'arrête aussitôt.
func Run(ctx context.Context, obsExe string, args ...string) error {
	cmd := exec.CommandContext(ctx, obsExe, args...)
	cmd.Dir = filepath.Dir(obsExe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("lancement d'OBS impossible : %w", err)
	}
	if err := cmd.Wait(); err != nil {
		// Un code de sortie non nul est fréquent quand l'utilisateur ferme la
		// fenêtre : ce n'est pas une erreur à remonter.
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return err
	}
	return nil
}
