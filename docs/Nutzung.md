# SecureCopy nutzen

Drei Werkzeuge, alle CLI. Die Sicherheit kommt aus der **PGP-Signatur** und den **Blake3-Prüfsummen**, nicht aus der Verbindung.

| Werkzeug | Wann | Aufgabe |
|---|---|---|
| `seccopy-publish` | vor dem Rsync, auf der vertrauenswürdigen Quelle | Index bauen und signieren |
| `seccopy-server` | im Air-Gap | Baum nur lesen und per gRPC ausliefern |
| `seccopy-client` | im Air-Gap | auflisten, holen, prüfen, ablegen |

Der öffentliche Schlüssel ist derselbe wie für den APT-Mirror und liegt auf den Rechnern bereits vor (Ansible). `-gpg-user` ist die Key-ID oder der Fingerprint dieses Schlüssels.

## Bauen

Voraussetzung: Go, `gpg`, der APT-Spiegel-Schlüssel im GnuPG-Schlüsselbund.

Direkt:

```text
go build -o seccopy-publish ./cmd/publish
go build -o seccopy-server  ./cmd/server
go build -o seccopy-client  ./cmd/client
```

Unter Windows entstehen `.exe`-Dateien. Für Linux (Ubuntu/Alpine) die Pakete bauen, siehe unten.

## Pakete

Es gibt drei Pakete plus ein Metapaket, jeweils als `.deb` (amd64) und `.apk` (x86_64):

| Paket | Inhalt |
|---|---|
| `seccopy-client` | `/usr/bin/seccopy-client` |
| `seccopy-server` | Binary, Startskript, `server.env.example`, systemd (Debian) bzw. OpenRC (Alpine) |
| `seccopy-publish` | `/usr/bin/seccopy-publish` |
| `seccopy` | hängt von den dreien ab |

Abhängigkeit: `gnupg`. Die Linux-Binaries werden ohne cgo gebaut und laufen auf glibc (Debian) und musl (Alpine).

### Pakete erzeugen

Auf Windows:

```text
powershell -File packaging/build.ps1
```

Auf Linux:

```text
sh packaging/build.sh
```

Ergebnis unter `dist/packages/`.

Auf GitHub baut Actions dieselben Pakete bei jedem Push auf `main` (und bei Pull Requests). Die Dateien liegen danach unter **Actions → aktueller Lauf → Artifacts → seccopy-packages**.

### Debian / Ubuntu

```text
sudo dpkg -i seccopy-client_0.1.0_amd64.deb
sudo dpkg -i seccopy-server_0.1.0_amd64.deb
sudo dpkg -i seccopy-publish_0.1.0_amd64.deb
```

Oder alle drei über das Metapaket, wenn die Dateien in einem Verzeichnis liegen und `apt` sie auflösen kann. Für den Alltag: die `.deb` in den bestehenden APT-Spiegel legen.

Server einrichten:

```text
sudo cp /etc/seccopy/server.env.example /etc/seccopy/server.env
sudo $EDITOR /etc/seccopy/server.env
sudo systemctl enable --now seccopy-server
```

In `server.env` mindestens `SECCOPY_GPG_USER` und `SECCOPY_ROOT` setzen. Der Dienst-User muss den APT-Public-Key in GnuPG haben.

### Alpine

Solange es keinen eigenen apk-Index gibt:

```text
sudo apk add --allow-untrusted seccopy-client_0.1.0_x86_64.apk
sudo apk add --allow-untrusted seccopy-server_0.1.0_x86_64.apk
sudo apk add --allow-untrusted seccopy-publish_0.1.0_x86_64.apk
```

(`--allow-untrusted` nur, bis die Pakete mit eurem Alpine-Key signiert bzw. in einem Index liegen.)

```text
sudo cp /etc/seccopy/server.env.example /etc/seccopy/server.env
sudo $EDITOR /etc/seccopy/server.env
sudo rc-update add seccopy-server
sudo rc-service seccopy-server start
```

Client und Publish brauchen keine Dienstdatei — nur das Binary und GnuPG.


## Typischer Ablauf

1. Admin legt den Verzeichnisbaum auf der USB-Quelle an (Dateien, Unterordner, Programme).
2. `seccopy-publish` schreibt `index.bin` und `index.bin.sig` **zuletzt** in diesen Baum.
3. `rsync` kopiert den ganzen Baum auf den Server im Air-Gap.
4. `seccopy-server` startet auf diesem Datenroot, prüft Signatur und Hashes.
5. Clients listen und holen. Jedes `get` holt den Index neu und prüft die Signatur erneut.

Die USB-Platte gilt als vertrauenswürdig. Der Server ist nur Verteiler.

## seccopy-publish

```text
seccopy-publish -gpg-user <APT-Key-ID> /pfad/zum/baum
```

| Flag | Standard | Bedeutung |
|---|---|---|
| `-gpg-user` | (pflicht) | Key-ID oder Fingerprint zum Signieren |
| `-repo` | `default` | Name des Repos (steht im Index) |
| `-gpg` | `gpg` | GPG-Binary, falls nicht im PATH |

Erzeugt im Baum:

- `index.bin` — Protobuf-Manifest (Pfad, Blake3, Größe, Unix-Mode)
- `index.bin.sig` — abgelöste PGP-Signatur

Diese beiden Dateien selbst werden nicht mitindexiert.

**Was aufgenommen wird**

- reguläre Dateien
- Symlinks (Ziel wird gespeichert, nicht gefolgt)

**Was abgelehnt wird** (Publish bricht ab, kein Index)

- Hardlinks
- Devices, Sockets, FIFOs
- Symlinks, die aus dem Baum zeigen (`..`, absolute Pfade)

Leere Ordner stehen nicht im Index. Der Client legt sie bei Bedarf aus den Dateipfaden an.

Danach den Baum per rsync auf den Server kopieren. Index und Signatur liegen bereits am Ende des Baums; rsync soll den Stand erst ausliefern, wenn der Lauf fertig ist. Den Server nach dem Rsync neu starten, damit er den neuen Index prüft.

## seccopy-server

```text
seccopy-server -gpg-user <APT-Key-ID> -listen :50051 /datenroot
```

| Flag | Standard | Bedeutung |
|---|---|---|
| `-gpg-user` | (pflicht) | erwarteter Unterzeichner |
| `-listen` | `:50051` | Bind-Adresse |
| `-repo` | `default` | muss zum Index passen |
| `-gpg` | `gpg` | GPG-Binary |
| `-user` / `-password` | leer | optionale Anmeldung |
| `-max-reads` | `256` | parallele Dateileser (File-Descriptor-Grenze) |

Beim Start: Signatur prüfen, Dateien parallel hashen. Dateien mit Hash-Mismatch werden nicht angeboten (`skipped` im Log). Ungültige Signatur → Prozess endet, nichts wird ausgeliefert.

Logs sind JSON auf stderr (für Grafana). Path-Traversal und ausbrechende Symlinks: Eintrag `security` und Verbindung zu.

Der Server liest nur aus `/datenroot`. Pfade mit `..` kommen nicht als Datei zurück.

Nur localhost binden, wenn der Zugang über SSH-Tunnel läuft:

```text
seccopy-server -gpg-user <APT-Key-ID> -listen 127.0.0.1:50051 /datenroot
```

Nach einem neuen Rsync den Server neu starten.

## seccopy-client

### Auflisten

```text
seccopy-client list -gpg-user <APT-Key-ID> -addr host:50051
seccopy-client list -gpg-user <APT-Key-ID> -addr host:50051 tools
```

Der Index bleibt nur im Speicher. Ausgabe auf stdout, eine Zeile pro Eintrag:

```text
f 0755     1234 tools/install.sh
l 0777       11 tools/current -> install.sh
```

`f` = Datei, `l` = Symlink. Nutzdaten der Dateien stehen nicht in dieser Ausgabe.

### Holen

```text
seccopy-client get -gpg-user <APT-Key-ID> -addr host:50051 -dest /ziel
seccopy-client get -gpg-user <APT-Key-ID> -addr host:50051 -dest /ziel tools
```

Ohne Prefix kommt der ganze Baum nach `/ziel` (Pfade wie im Repo: `/ziel/tools/install.sh`). Mit Prefix nur dieser Unterbaum, die Relativpfade bleiben erhalten (`/ziel/tools/...`).

| Flag | Standard | Bedeutung |
|---|---|---|
| `-gpg-user` | (pflicht) | erwarteter Unterzeichner |
| `-dest` | (pflicht bei `get`) | Zielverzeichnis |
| `-addr` | `127.0.0.1:50051` | Server |
| `-repo` | `default` | Repo-Name |
| `-gpg` | `gpg` | GPG-Binary |
| `-user` / `-password` | leer | nur wenn der Server das verlangt |

Lokale OS-Rechte gelten: der Client kann nur schreiben, wohin der User darf. Unix-Mode aus dem Index (inkl. Executable) wird gesetzt, Owner/Group nicht.

Ablauf bei `get`:

1. Index holen, Signatur prüfen.
2. Jede Datei in eine Temp-Datei (Mode `0600`, nicht ausführbar).
3. Größe und Blake3 prüfen.
4. Erst dann atomar an den Zielort, danach Rechte aus dem Index.

Schlägt irgendetwas fehl, werden Temp-Dateien **und** alle in diesem Lauf schon abgelegten Dateien genullt und gelöscht. Es bleibt nichts Verwertbares aus diesem Lauf liegen.

Nutzdaten gehen nicht nach stdout — kein `get | bash`.

## SSH-Tunnel

Es gibt keinen eingebauten SSH-Modus. Der übliche Weg:

```text
ssh -L 50051:127.0.0.1:50051 user@airgap-host
seccopy-client list -gpg-user <APT-Key-ID> -addr 127.0.0.1:50051
seccopy-client get  -gpg-user <APT-Key-ID> -addr 127.0.0.1:50051 -dest /ziel
```

Der Server sollte dann nur auf `127.0.0.1` lauschen. Verschlüsselt ist der Weg; vertrauenswürdig bleibt die Signatur.

## Key-ID finden

```text
gpg --list-keys
```

Dieselbe ID verwenden, mit der der APT-Mirror signiert wird, z. B.:

```text
-gpg-user 0xDEADBEEF
```

oder der volle Fingerprint bzw. die E-Mail-Adresse des Schlüssels, wenn GnuPG sie eindeutig zuordnet.

## Fehlerbilder

| Symptom | Typische Ursache |
|---|---|
| `gpg-user is required` | Flag vergessen |
| `index signature` / `gpg verify` | falscher Key, fehlender Public Key, beschädigte Signatur |
| `blake3 mismatch` / Wipe | Datei passt nicht zum Index; Client räumt den Lauf weg |
| `hardlinks are not allowed` | Publish: Hardlink im Baum |
| `escapes the tree` | Symlink zeigt aus dem Repo |
| `no matching entries` | Prefix existiert nicht |
| `unknown repository` | `-repo` weicht vom Index ab |
| Server startet nicht | Signatur ungültig oder Index fehlt (`index.bin` / `index.bin.sig`) |

Path-Traversal gegen den Server erscheint im JSON-Log als `"event":"path_traversal"` bzw. `"symlink_escape"` und sollte in Grafana alarmieren.
