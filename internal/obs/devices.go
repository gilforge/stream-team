package obs

import "sort"

// localKeys énumère, par type de source, les réglages qui décrivent un appareil
// physique et qui ne doivent donc jamais traverser le serveur.
//
// Une source « Webcam » ne mémorise pas « une webcam » mais un identifiant
// matériel précis, du genre :
//
//	"video_device_id": "Logitech C920 HD Pro\{5d5ec5d2-...}"
//
// Recopier tel quel chez un membre équipé autrement donne un écran noir. Il
// reconfigure, la synchronisation suivante réimpose l'appareil du publieur, et
// l'équipe abandonne l'outil au bout de deux fois.
//
// Seules ces clés d'identification restent locales. La résolution de la webcam,
// son cadrage, ses filtres, son volume, son délai audio : tout le reste
// appartient à l'équipe et se synchronise normalement.
var localKeys = map[string][]string{
	"dshow_input": {
		"video_device_id", "audio_device_id",
		"last_video_device_id", "last_audio_device_id",
	},
	"wasapi_input_capture":           {"device_id"},
	"wasapi_output_capture":          {"device_id"},
	"wasapi_process_output_capture":  {"window"},
	"monitor_capture":                {"monitor", "monitor_id"},
	"display_capture":                {"display", "display_uuid", "monitor_id"},
	"window_capture":                 {"window"},
	"game_capture":                   {"window"},
	"av_capture_input":               {"device", "device_name"}, // macOS
	"coreaudio_input_capture":        {"device_id"},             // macOS
	"pipewire-screen-capture-source": {"RestoreToken"},          // Linux/Wayland
}

// IsHardware indique si un type de source dépend du matériel de la machine.
func IsHardware(sourceType string) bool {
	_, ok := localKeys[sourceType]
	return ok
}

// LocalKeysFor expose les clés machine d'un type de source, pour neutraliser
// une collection avant de la comparer d'un poste à l'autre.
func LocalKeysFor(sourceType string) []string {
	return localKeys[sourceType]
}

// Extract relève les réglages matériels de cette machine, pour pouvoir les
// réinjecter après chaque réception.
//
// Appelé à la fermeture d'OBS : si un membre change de webcam, le nouveau
// réglage est capturé sans qu'il ait rien à déclarer.
func Extract(c *Collection) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, s := range c.Sources() {
		keys, ok := localKeys[s.Type()]
		if !ok {
			continue
		}
		settings := s.Settings()
		if settings == nil {
			continue
		}
		captured := map[string]any{}
		for _, k := range keys {
			v, present := settings[k]
			if !present || isEmpty(v) {
				continue
			}
			captured[k] = v
		}
		if len(captured) > 0 {
			out[s.Name()] = captured
		}
	}
	return out
}

// Apply réinjecte les réglages mémorisés dans une collection fraîchement reçue,
// et renvoie les sources pour lesquelles cette machine n'a encore rien : ce sont
// celles que le dock affichera comme « à configurer ».
//
// Une source matérielle sans réglage mémorisé est toujours signalée, même si la
// collection partagée porte une valeur : cette valeur désigne l'appareil de
// quelqu'un d'autre et ne fonctionnera pas ici.
func Apply(c *Collection, overrides map[string]map[string]any) (unconfigured []string) {
	for _, s := range c.Sources() {
		keys, ok := localKeys[s.Type()]
		if !ok {
			continue
		}
		mine, known := overrides[s.Name()]
		if !known || len(mine) == 0 {
			unconfigured = append(unconfigured, s.Name())
			continue
		}
		settings := s.EnsureSettings()
		for _, k := range keys {
			if v, present := mine[k]; present {
				settings[k] = v
			} else {
				// La valeur du publieur désignerait son matériel : mieux vaut
				// la retirer et laisser OBS reprendre son défaut.
				delete(settings, k)
			}
		}
	}
	sort.Strings(unconfigured)
	return unconfigured
}

// Merge complète les réglages mémorisés sans effacer ceux d'une source absente
// de la collection courante : un membre peut avoir plusieurs collections, et
// une source retirée puis remise doit retrouver son appareil.
func Merge(existing, fresh map[string]map[string]any) map[string]map[string]any {
	if existing == nil {
		existing = map[string]map[string]any{}
	}
	for name, settings := range fresh {
		existing[name] = settings
	}
	return existing
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == "" || t == "default"
	}
	return false
}
