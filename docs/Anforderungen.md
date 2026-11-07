# SecureCopy

## Problem

In ein Air-Gap-Netz müssen Programme und Dateien eingebracht werden. Dabei muss verhindert werden, dass diese Dateien unbemerkt verändert werden.

Das Modell ist APT-ähnlich, aber für Verzeichnisbäume: ein Repo mit Ordnern, Dateien und Programmen, dazu eine signierte Indexliste (Pfad relativ zum Repo plus Prüfsumme). Ein Client listet den Inhalt und lädt rekursiv. Die Prüfung erfolgt, bevor etwas als nutzbare Datei existiert. Schlägt sie fehl, werden die Daten genullt und sofort gelöscht.

Die Sicherheit kommt aus den Signaturen, nicht aus der Verbindung. TLS kann später ergänzt werden.

## Vertrauensmodell

- Die Daten auf der USB-Festplatte gelten als vertrauenswürdig.
- Der Server sitzt **im Air-Gap**. Er ist Verteiler, nicht Vertrauensanker.
- Der Public Key ist derselbe wie für den APT-Mirror und liegt auf den Clients bereits vor (per Ansible).
- Subkeys pro Admin (Masterkey beglaubigt Signing-Keys) sind eine spätere Option, nicht Teil des MVP.
- Client und Server werden als Pakete verteilt, nicht über SecureCopy selbst. Eine Prüfung „ist der Client ein signiertes Binary?“ entfällt.

## Komponenten

Drei CLI-Werkzeuge, alle in Go. Keine Web-Oberfläche.

| Werkzeug | Wo | Aufgabe |
|---|---|---|
| `publish` | Admin / Quelle vor dem Rsync | Baum einlesen, Index bauen, signieren |
| `server` | Air-Gap | Nur aus seinem Datenroot lesen, per gRPC ausliefern |
| `client` | Air-Gap | Listen, rekursiv holen, prüfen, atomar ablegen |

Paketierung: `.deb` (Ubuntu/Debian) und `.apk` (Alpine), siehe [docs/Nutzung.md](Nutzung.md#pakete).

## Ablauf

1. Ein Admin bereitet den Baum vor und führt `publish` aus.
2. `publish` schreibt den signierten Index **zuletzt** und **atomar** (Index und Signatur ersetzen, nachdem alle Dateien stehen).
3. Der fertige Baum wird per **rsync** auf den Server im Air-Gap kopiert.
4. Der Server prüft den Baum **beim Start** (Signatur, Hashes).
5. Der Client holt den Index frisch, prüft die Signatur, lädt Dateien, prüft Blake3, legt erst dann an den Zielort.

## Index

Format: **Protobuf** (kompakt, dieselben Types wie die gRPC-API).

Pro Eintrag:

- Pfad relativ zum Repo
- Blake3
- Dateigröße
- Unix-Mode (Rechte inklusive Executable-Bit)

Nicht im Index:

- Owner und Group
- leere Verzeichnisse (werden beim Auspacken aus den Dateipfaden erzeugt)

Ein Index beschreibt den ganzen Baum. Keine separaten Snapshots/Versionen im MVP.

Signatur: PGP über die Index-Bytes, als abgelöste Signatur neben dem Index (`index.bin` + `index.bin.sig` oder gleichwertig).

## Dateitypen beim Publish

| Typ | Verhalten |
|---|---|
| Reguläre Datei | aufnehmen |
| Symlink | Ziel speichern, nicht folgen |
| Leeres Verzeichnis | ignorieren |
| Hardlink | ablehnen, Publish bricht ab |
| Device, Socket, FIFO, sonstige Sonderdateien | ablehnen, Publish bricht ab |

Symlink-Ziele, die aus dem Baum zeigen (`..`, absolute Pfade außerhalb), sind im Normalbetrieb unmöglich. `publish` lehnt sie ab. Sieht der **Server** so etwas (oder Path-Traversal in einer Anfrage), **beendet er die Verbindung sofort** und schreibt ein Log. Das Log geht weiter an Grafana und löst dort einen Alert aus. Ein zusätzliches Sperren von Accounts/IPs ist nicht nötig.

## Transport

- gRPC (Binärdaten).
- Repo-Name ist in der API vorgesehen; MVP startet mit einem Repo (`default`).
- Benutzer und Kennwort optional.
- Sicherheit der Inhalte: Signatur und Hashes. TLS ist optional und nicht der Vertrauensanker.

## Client

- Zielverzeichnis wählt der User. Die lokalen OS-Rechte schränken ihn ein.
- Es kann der ganze Baum oder nur ein Unterverzeichnis geholt werden.
- Rechte aus dem Index werden gesetzt (Mode, inkl. Executable). Owner/Group nicht.
- Nutzdaten gehen **nicht** nach stdout (kein Pipe-Missbrauch).
- Dateien können größer als der RAM sein. Schreiben nur in Temp-Dateien (Mode `0600`, nicht ausführbar, nicht im Zielpfad).
- Nach erfolgreicher Prüfung: atomar an den Zielort, dann Rechte aus dem Index.
- `list` hält den Index nur im Speicher. `get` holt den Index immer erneut und prüft die Signatur erneut. Kein Index-Cache auf der Platte.

## Fehler und Integrität

Sicherheit geht vor. Wenn irgendetwas nicht stimmt:

- betroffene Temp-Dateien mit Nullen überschreiben und sofort löschen
- bereits in diesem Lauf geholte Dateien ebenfalls nullen und löschen
- nichts Verwertbares am Zielort hinterlassen

Der Client prüft Signatur und Blake3 immer. Der Server prüft den Baum beim Start; Dateien mit Hash-Mismatch werden nicht angeboten.

## Server-Grenzen

- Liest ausschließlich aus seinem Datenroot.
- Pfade, die das Verzeichnis verlassen sollen, führen zum sofortigen Verbindungsabbruch und einem Log-Eintrag (Grafana-Alert).
- Dasselbe gilt für Symlink-Ziele außerhalb des Baums.

## Später (nicht MVP)

- OpenPGP-Subkeys pro Admin, Prüfung über den Masterkey
- mehrere Repos (API ist vorbereitet)
- TLS
- Alpine-Pakete, sobald ein eigener apk-Index betrieben wird (die `.apk`-Dateien selbst sind vorhanden)
