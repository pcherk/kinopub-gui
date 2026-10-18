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
