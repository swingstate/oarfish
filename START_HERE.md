# Oarfish Starter Pack v0.3

Dieses Paket enthält die freigegebene Arbeitsgrundlage für Gate G1.

## Inhalt

- `docs/PFLICHTENHEFT.md`: normative Source of Truth
- `CLAUDE.md`: kompakte Projektregeln für Claude Code
- `docs/prompts/G1_REPOSITORY_AND_CI.md`: erster ausführbarer Gate-Prompt
- `docs/adr/`: bestätigte Architekturentscheidungen
- `docs/DECISIONS.md`: bestätigte und später fällige Entscheidungen
- `docs/ROADMAP.md`: Gate-Reihenfolge

## Start auf dem MacBook

```bash
mkdir -p ~/Projects/oarfish
cd ~/Projects/oarfish
unzip /PFAD/ZU/Oarfish_Starter_Pack_v0.3.zip

git init -b main
claude
```

In Claude Code den Inhalt von `docs/prompts/G1_REPOSITORY_AND_CI.md` verwenden.

Gate G1 erstellt nur Repository-Skeleton und CI. Es erstellt noch keine Release-Pipeline, keinen Installer und keine Verbindung zum Raspberry Pi.

## Verbindliche Baseline

- Referenzgerät: Raspberry Pi Zero 2 W
- Initiales OS: Raspberry Pi OS Lite 64-bit
- Verbindliches Folgeziel: Ubuntu Server LTS ARM64
- Entwicklung: Claude Code auf dem MacBook
- CI: GitHub-hosted Actions Runner
- Distribution: GitHub Releases
- Erstinstallation: pull-basiertes `install.sh`
- Updates: pull-basiert aus der Oarfish-GUI
- Tailscale: optionales Plugin
- Kein Self-hosted Runner
- Kein SSH-Deployment aus GitHub Actions
