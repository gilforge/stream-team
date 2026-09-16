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

## Amorcer une régie

Un dossier neuf chez l'hébergeur ne contient rien : il n'y a donc rien à
recevoir, et la première version doit y être déposée. Depuis le poste qui a son
`publisher.json` :

```sh
stream-team -publish -m "mise en place"
```

La collection locale part telle quelle en v1, avec les overlays du dossier
d'assets. L'équipe peut ensuite se contenter de lancer le programme.

## Vérifier une configuration

```sh
stream-team -check
```

Synchronise puis s'arrête, sans ouvrir OBS. Utile pour valider une adresse de
régie, voir ce qui est téléchargé et quelles sources restent à configurer, avant
de confier l'outil à toute une équipe.

Deux conseils sur l'adresse de lecture : la donner **avec sa barre oblique
finale**, et sous sa forme définitive. Beaucoup d'hébergements redirigent le
domaine nu vers `www.` — partir directement de l'adresse d'arrivée évite une
redirection à chaque requête.

## Construire et tester

```sh
go build ./cmd/stream-team
go test ./...
```

Construit et vérifié avec Go 1.27 sous Windows ; `go vet` est propre.

Les dépendances sont **vendorisées** dans `vendor/` : le dépôt se compile hors
ligne, sans rien télécharger et sans dépendre d'un cache de modules ailleurs sur
la machine. Seul le compilateur Go est requis.

Les tests couvrent le relevé et la réinjection des périphériques, la
tokenisation des chemins, la stabilité de la normalisation JSON, la réécriture
ciblée du `basic.ini`, et une réception de bout en bout contre une régie servie
en HTTP statique.

`internal/obs/real_test.go` confronte en plus le code aux collections réellement
présentes sur la machine — en lecture seule, et sauté s'il n'y a pas d'OBS
installé. C'est ce test qui a révélé le traitement fautif de `device_id:
"default"`.

## À faire

Relevé en éprouvant l'outil sur une vraie équipe, par ordre de gêne constatée.

- **Sélectionner la collection au lancement.** L'agent écrit bien le fichier
  avant d'ouvrir OBS, mais OBS rouvre la dernière collection utilisée sur la
  machine. Sur un poste neuf, il faut donc basculer à la main une première fois
  via `Collection de scènes`, sans quoi on croit que rien n'est arrivé. Se joue
  dans la configuration globale d'OBS, à ne pas modifier à l'aveugle.
- **Refuser une adresse sans manifeste quand le poste n'est pas publieur.**
  L'assistant tolère une régie qui ne répond pas, pour permettre d'en amorcer
  une vide. Mais un membre qui se trompe d'adresse s'en aperçoit trop tard.
  La présence de `publisher.json` distingue déjà les deux cas.
- **Vérifier que la scène active ne compte pas comme une modification.** OBS
  enregistre `current_scene` dans la collection : si basculer de scène pendant
  un stream fait passer le dock en « modifié », il faut exclure ces champs du
  calcul d'empreinte, sinon l'outil réclame une publication après chaque
  session.
- Inscription automatique du dock dans la configuration d'OBS (même prudence
  que pour la collection : écrire au mauvais endroit casserait la
  configuration de l'utilisateur).
- Assistant de premier lancement en fenêtre plutôt qu'en console.
- Retour à une version antérieure depuis le dock (`versions/` est déjà rempli).
- Localisation de l'exécutable d'OBS par la base de registre, en complément des
  emplacements usuels.

## Où en est l'outil

Éprouvé de bout en bout sur un cas réel : régie publiée en v1 sur un
hébergement mutualisé o2switch (26 assets, 50 Mo), reçue sur un second poste.
32 tests passent, `go vet` est propre, le dépôt se compile hors ligne.

Reste à confirmer sur la durée : le cycle complet publier/recevoir entre deux
membres au fil de vraies sessions de stream.

## Hébergements mutualisés : deux pièges du FTPS

Éprouvé contre un hébergement o2switch, les deux se retrouvent ailleurs.

**Le certificat est celui du serveur, pas du domaine.** `ftps://…@ftp.mondomaine.fr`
échoue parce que le certificat couvre `araucaria.o2switch.net`. Mettre ce
nom-là dans `write_url` règle le problème, sans rien désactiver :

```
ftps://identifiant@araucaria.o2switch.net/public_html/CKZ/twitch
```

**Le canal de données doit reprendre la session TLS du canal de contrôle.**
Sans cela le serveur avorte chaque transfert par un `451 Transfer aborted`
après quelques octets. Le client s'en charge (cache de sessions TLS, TLS 1.2) ;
c'est documenté ici parce que le symptôme est déroutant.

Enfin, ces hébergements limitent le débit de requêtes : un `429` est repris
automatiquement, jusqu'à trois tentatives, en respectant `Retry-After`.
