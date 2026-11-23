# Hypercube

Minimal Minecraft: Java Edition launcher for Windows. Instances per game version and mod loader, mod lists with categories, Modrinth and CurseForge browsing.

## Features

| Area | Details |
| --- | --- |
| Instances | Any release, snapshot or legacy version with Vanilla, Fabric, Quilt, Forge or NeoForge |
| Runtime | Downloads the game, libraries, assets and the matching Mojang Java runtime |
| Mods | Name, version, description, authors and icon read from each jar (`fabric.mod.json`, `quilt.mod.json`, `mods.toml`, `neoforge.mods.toml`, `mcmod.info`); missing icons resolved on Modrinth by SHA-1 |
| Organization | Categories (drag and drop, reorder, collapse), multiple mod lists per instance, copy mods between instances |
| Browse | Modrinth (no setup) and CurseForge (API key) for mods, resource packs and shaders, filtered by instance version and loader, required dependencies included |
| Profile | Local launcher profile (name and picture, GIF supported), `.hcbackup` export and import |
| Account | Microsoft sign-in (device code flow) with Minecraft: Java Edition ownership check; required to play |

## Requirements

| | |
| --- | --- |
| Build | Go 1.22+ (standard library only) |
| Run | Windows 10/11 with Microsoft Edge (Chrome or Brave as fallback) |

## Build

```
build.bat
```

```
sh build.sh
```

Output: `dist/Hypercube.exe`. Windows resources (icon, version info, manifest) live in `rsrc_windows_amd64.syso`; regenerate with:

```
go run tools/winres/main.go
```

## Microsoft sign-in

Mojang requires every third-party launcher to use its own Azure application.

1. Azure portal → App registrations → New registration → *Personal Microsoft accounts only*.
2. Authentication → *Allow public client flows* → Yes.
3. Request Minecraft API access: https://help.minecraft.net/hc/en-us/articles/16254801392141
4. Paste the Application (client) ID in Settings → Minecraft account.

## Data

`%APPDATA%\Hypercube`

| Path | Content |
| --- | --- |
| `instances/<id>/minecraft/` | Game directory (mods, config, saves) |
| `instances/<id>/instance.json` | Instance, categories and mod lists |
| `shared/` | Versions, libraries, assets and Java runtimes |
| `cache/` | Mod icons and metadata |
| `settings.json` | Settings, profile and account tokens |

## Layout

| Path | Purpose |
| --- | --- |
| `main.go`, `api.go`, `events.go` | Local HTTP server, API and event stream |
| `mojang.go`, `java.go`, `launch.go`, `log4j.go` | Versions, runtimes and game launch |
| `loaders.go` | Fabric, Quilt, Forge and NeoForge installation |
| `mods.go`, `remote.go` | Jar metadata, mod lists, Modrinth and CurseForge |
| `profile.go`, `mcauth.go` | Local profile, backups and Microsoft sign-in |
| `platform_*.go` | Windows window, icon and shell integration |
| `web/` | Embedded interface |
| `assets/` | Icon sources |
| `tools/winres/` | Windows resource generator |

## License

Fonts: Geist, SIL Open Font License (`web/fonts/LICENSE.txt`).
