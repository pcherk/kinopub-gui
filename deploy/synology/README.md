# KinoPub GUI — Synology NAS

Canonical source: https://github.com/pcherk/kinopub-gui (branch `synology`).
Upstream: https://github.com/ZioSHik/kinopub-gui

This build keeps Synology's settings and downloads in external bind mounts,
and is based on a fixed release tag rather than rebuilding arbitrary upstream
code on each deploy. No sed/patch/COPY of Go code is required.

## Upgrading an existing Synology installation

1. Verify the current container works and downloads are not in progress.
2. Back up the folders `state`, `secrets` and `downloads`, and `.env`.
   In particular, `state/machine-id` is needed to decrypt stored OAuth tokens.
3. Copy `Dockerfile` and `compose.yaml` from `deploy/synology` into
   the existing NAS folder `/docker/kinopub`. Leave all persistent folders unchanged.
4. Start a project rebuild in Synology Container Manager, using the *existing*
   project and directory. The built image tag changes to `synology-v1.1.0-1`.
5. Verify web login, active kino.watch session, queue and film download.
6. Roll back by restoring the prior `Dockerfile` and `compose.yaml` and rebuilding;
   do not delete or recreate data folders.

## Security

The NAS Go source has HTTP Basic Auth and a strict HTTP Host allowlist.
The allowlist requires `KINOPUB_REMOTE_ALLOWED_HOSTS` and is configured by
Compose with the NAS LAN IP and port from `.env`.

Basic Auth over HTTP is only appropriate on an isolated trusted LAN or VPN.
For external access use HTTPS terminated by Synology reverse proxy with
corresponding allowed host configuration. Do not port-forward TCP 8765.

OAuth HTTP diagnostics contain only request grant type and HTTP status.
Transport errors omit URLs that may contain tokens. The existing historical
Docker logs may contain previously disclosed tokens: revoke those sessions
in kino.watch and keep old log files private. Rotating Docker logs cannot
revoke issued credentials.

Built-in GUI binary updates and dependency installers are blocked in NAS mode;
update by rebuilding the pinned Docker image.

## Data and permissions

The container runs as Synology UID 1026 / GID 100 by default. These IDs must
have read and write permissions on `state` and `downloads`, and read access
to `secrets/password`. The encrypted credential store is inside
`state/.config/kinopub/credentials.enc`.

Existing media path is preserved:
`./downloads:/data/Downloads/kinopub`. The empty folder
`state/Downloads/kinopub` is a nested bind mount point; do not delete it
while the container is running.

For future versions:
- upstream updates are merged into the maintained `synology` branch,
- full tests and a Docker build are run,
- a new release tag is created and pinned in Dockerfile / Compose.


## Local browser playback (v1.1.0-2)

The downloaded-media Play button no longer asks the Synology container to
launch `xdg-open`. Instead, the authenticated GUI serves local files itself:

- MP4/M4V/WebM: byte-range HTTP streaming to the browser's native video element.
- MKV: FFmpeg prepares an HLS rendition in the existing state volume's
  `.cache/kinopub-local-playback` directory. H.264 video is copied without
  re-encoding; other video codecs are encoded to H.264 with two FFmpeg threads.
- HLS segments are subject to cleanup after 30 minutes of inactivity.
- Only media located inside configured download/library roots can be read.
  Paths and symlinks escaping those roots are denied.
- The source media is never modified or replaced.

The first MKV playback can take longer; seeking is available within already
prepared HLS segments until the background conversion finishes. Some very large
or nonstandard MKV files may require significant CPU time and free disk space.
For MP4 with unusual incompatible codecs, browser playback can still fail;
the MP4 container alone does not guarantee browser codec compatibility.

For existing Synology installations, replace only Dockerfile/compose.yaml and
rebuild the project. Keep state/, downloads/, secrets/ and .env unchanged.

## Synology Compose compatibility

The per-service `logging:` stanza was removed because it was rejected by the
user's Synology Container Manager. Synology/Docker handles log storage using
its own runtime defaults. Check DSM Container Manager for journal growth or
configure daemon-wide rotation if the installed Docker version supports it.
No Go application changes, storage changes or image rebuild are needed.


## Prepare for iPad (v1.1.0-3)

In Offline Library use **Для iPad** on a downloaded movie or single episode.
The NAS prepares an iPad-friendly MP4 file with defaults:

- H.264 (maximum 1080p), AAC stereo 160 kb/s, `+faststart`, CRF 23.
- When H.264/yuv420p is already suitable, copy the video without recoding.
- One preparation runs at a time (limit NAS load).
- Progress and cancellation are available through the UI.
- Finished copies are preserved under `state/iPad-Exports/<hash>/`,
  separate from the original films. Repeated preparation reuses existing copies.
- **Скачать на iPad** sends a normal HTTP attachment that Safari saves to Files.
- **Удалить подготовленную копию** removes only the export, never the source.
- Existing mount paths and credentials are unchanged. No Web Station dependency.

On iPad: open KinoPub in Safari, start the preparation, wait for completion,
then tap Download and confirm the download in Safari. The downloaded MP4 can
be played offline using iPadOS Files. When accessing over a public hostname,
Synology Reverse Proxy and HTTPS must already work. This build allows
`kino.pac.wiki` as well as the local LAN IP; change PUBLIC_HOST to override.

The first audio track is used. Multi-language audio and embedded subtitles are
not carried to the iPad preset. Complex/long files may take substantial CPU
time or additional disk space. Completed MP4 copies remain until explicitly
deleted from the UI. Do not delete `state/iPad-Exports` during conversion.


## FIFO iPad preparation queue — v1.1.0-4

The NAS still uses software FFmpeg, because DS1621xs+ / Intel Xeon D-1527 has
no integrated Intel Quick Sync GPU. Multiple movies and episodes can be added:
the first is processed, others wait FIFO (one FFmpeg conversion at a time).
Re-adding the same movie does not duplicate it. Pending jobs survive a
container restart in `state/.config/kinopub/ipad-queue.json`; interrupted
running conversions are restarted from the beginning because ordinary MP4
encoding cannot resume a partial output.

**Location changes for NEW exports only:** all new iPad MP4s appear under
`/docker/kinopub/downloads/iPad/<Title>/<Title> - S01E01 - iPad.mp4`,
using the existing writable `./downloads` bind mount; no new volume or ACL
setup is needed. The old `state/iPad-Exports` folder, including any previously
finished MP4 or currently incomplete `.part`, is intentionally NOT moved,
overwritten or deleted by an update. Let active exports finish before changing
versions, and clean up old files only after verifying they are no longer needed.

The list/grid still has a "Для iPad" action for each movie/episode. Use
"Очередь iPad" in the Offline Library header to see running, queued, finished
and failed jobs, download prepared files, or cancel an individual queued job.
The NAS continues working if you close Safari or the dialog.

No unsupported per-service `logging` stanza was introduced.
