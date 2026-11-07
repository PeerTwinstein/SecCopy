# SecureCopy

Signiertes Verzeichnis-Repo für ein Air-Gap: Dateien kommen nur an, wenn sie zum PGP-signierten Index passen.

- Spezifikation: [docs/Anforderungen.md](docs/Anforderungen.md)
- Bedienung und Pakete: [docs/Nutzung.md](docs/Nutzung.md)

```text
go build -o seccopy-publish ./cmd/publish
go build -o seccopy-server  ./cmd/server
go build -o seccopy-client  ./cmd/client
```

Debian- und Alpine-Pakete:

```text
powershell -File packaging/build.ps1
```
