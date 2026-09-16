# stream-team

Synchronise les scènes OBS d'une équipe de streamers. Un membre lance le
raccourci, ses scènes sont à jour, OBS s'ouvre. En quittant, ses modifications
partent sur la régie s'il l'a demandé.

Pas de serveur à administrer : la lecture se fait en HTTP sur n'importe quel
hébergement web, l'écriture en FTP, FTPS ou SFTP chez la seule personne qui
publie.

## Pourquoi un lanceur plutôt qu'un plugin

OBS charge ses scènes en mémoire au démarrage et ne réécrit ses fichiers qu'à la
fermeture. Écrire pendant qu'il tourne revient à voir son travail écrasé à la
sortie. L'agent n'agit donc qu'aux deux moments où le disque fait foi : avant le
lancement, après la fermeture.

Il en découle qu'il n'a aucune raison d'exister quand OBS ne tourne pas. Rien au
démarrage de Windows, aucun service, aucun installeur — un dossier et un
raccourci, supprimables d'un bloc.

## Installation

1. Télécharger `stream-team.exe` depuis les Releases.
2. Le poser dans un dossier et le lancer. Il demande l'adresse de la régie, le
   dossier local des overlays et le nom de la collection OBS.
3. Dans OBS : `Docks → Dock de navigateur personnalisé`, coller l'adresse
   affichée (`http://127.0.0.1:47838`).
4. Régler webcam et micro une fois. Ils sont conservés à chaque mise à jour.

Ensuite, lancer OBS **par ce programme** et non par son raccourci habituel.

## Devenir publieur

Poser un `publisher.json` à côté de l'exécutable :

```json
{
  "write_url": "ftps://marc@exemple.fr/www/streamteam",
  "password": "…",
  "author": "Marc"
}
```

Le binaire est identique pour tout le monde : c'est la présence de ce fichier,
et rien d'autre, qui fait apparaître les boutons de publication. Sans lui, le
dock est en lecture seule et publier par accident est impossible.

`write_url` accepte trois protocoles :

| Schéma   | Transport | Remarque |
|----------|-----------|----------|
| `ftp://`  | en clair | identifiants exposés sur le réseau, à éviter |
| `ftps://` | FTP + AUTH TLS | ce que proposent la plupart des hébergeurs |
| `sftp://` | sur SSH | `key_file` pour une clé privée sans phrase secrète |

En SFTP, l'empreinte du serveur est mémorisée au premier contact dans
`known-hosts.json` et vérifiée ensuite. Un changement d'empreinte bloque la
connexion.

## Ce qui voyage, ce qui reste

| | |
|---|---|
| Scènes, sources, filtres, transitions, mixage | partagé |
| Overlays et autres assets | partagé, en différentiel par empreinte |
| Résolution de canvas et FPS | partagé — des canvas différents décalent toutes les positions |
| Clé de flux | **jamais** — saisie une fois dans OBS |
| Encodeur (NVENC / AMF / x264) | local |
| Webcam, micro, capture d'écran ou de fenêtre | local |
| Thème, disposition des docks, raccourcis | local |

Les identifiants d'appareils (`video_device_id`, `device_id`, `monitor_id`,
`window`) sont relevés à la fermeture d'OBS et réinjectés après chaque
réception. Sans ce mécanisme, chaque mise à jour imposerait la webcam du
publieur et donnerait un écran noir à toute l'équipe.

Les chemins d'assets voyagent sous la forme `$ASSETS$/…` et sont réécrits vers
le dossier local de chaque machine.

## Organisation du code

```
cmd/stream-team      orchestration : recevoir → OBS → publier
internal/config      config.json, publisher.json, local-overrides.json
internal/manifest    l'état publié : version, empreintes, canvas
internal/storage     Reader HTTP (tous) et Writer FTP/FTPS/SFTP (le publieur)
internal/obs         fichiers d'OBS : chemins, collection, périphériques, canvas
internal/pipeline    réception et publication
internal/dock        le panneau servi sur 127.0.0.1
internal/launcher    démarrage d'OBS et attente de sa fermeture
```

## Cohérence de la publication

Sans WebDAV, il n'y a ni écriture conditionnelle ni copie côté serveur. La
garantie vient de l'ordre : archiver, déposer les assets, **puis le manifeste en
dernier**. Tant que le manifeste n'a pas changé, les autres postes voient la
version précédente, même si les nouveaux fichiers sont déjà en ligne.

Le manifeste est lu avec un paramètre anti-cache : servi depuis un cache, il
ferait croire à un membre qu'il est à jour alors qu'il ne l'est pas.

## Hypothèses assumées

L'outil suppose **un seul publieur** et pas d'édition concurrente. Deux versions
d'une collection de scènes ne se fusionnent pas : le dernier qui publie a
raison. Une publication partie d'une version périmée est refusée, et la
divergence locale est signalée dans le dock — mais il n'y a ni verrou ni fusion.

## Construire

```sh
go mod tidy
go build ./cmd/stream-team
```

Construit et vérifié avec Go 1.27 sous Windows. `go vet` est propre.

## À faire

- Assistant de premier lancement en fenêtre plutôt qu'en console.
- Inscription automatique du dock dans la configuration d'OBS (la clé exacte de
  `global.ini` / `user.ini` reste à vérifier sur plusieurs versions ; écrire au
  mauvais endroit casserait la configuration de l'utilisateur).
- Retour à une version antérieure depuis le dock (`versions/` est déjà rempli).
- Localisation de l'exécutable d'OBS par la base de registre, en complément des
  emplacements usuels.
