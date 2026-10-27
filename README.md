<p align="center">
  <img src="resources/icon.png" width="128" height="128" alt="YTGrab icon">
</p>

# YTGrab

YTGrab is a small macOS app for downloading video and audio with
[yt-dlp](https://github.com/yt-dlp/yt-dlp), without assembling command-line
arguments. It ships with yt-dlp and FFmpeg inside, so nothing needs to be
installed, and it keeps itself and yt-dlp up to date.

## Features

- Download video or extract audio
- Choose a maximum resolution and MP4 or MKV output
- Export MP3, M4A, Opus, FLAC or WAV audio
- Download playlists or a selected time range
- Embed metadata, cover art and English subtitles
- Access signed-in content with pasted cookies or cookies from a browser
- View live progress and the full yt-dlp log
- Cancel downloads and reveal finished files in Finder

## Staying up to date

- **yt-dlp** updates itself, because sites change often enough that a fixed
  copy stops working within weeks.
  - On first launch, YTGrab copies its bundled yt-dlp to
    `~/Library/Application Support/YTGrab/bin/`.
  - It checks that copy for a new stable release once a day, using yt-dlp's
    own updater, which verifies release checksums.
  - You can also update from **YTGrab → Update yt-dlp**, or with the
    **Update yt-dlp** button after a failed download.
  - A yt-dlp you choose with **Locate…** is left alone.
- **YTGrab** updates itself from this repository's
  [releases](https://github.com/aatng-gh/ytgrab/releases).
  - It checks daily, or from **YTGrab → Check for Updates…**.
  - Updates are signed, and the app checks the signature before installing.
  - The app must be in `/Applications` or `~/Applications` to update itself.

## Install

Download the disk image from the latest release and drag YTGrab to
Applications. It needs macOS 14 or newer on Apple silicon.

The app is ad-hoc signed, not notarized, so macOS blocks it the first time.
Open it once from **System Settings → Privacy & Security → Open Anyway**.
Later updates don't ask again.

## Build

You need only Go 1.27 or newer.

```sh
./scripts/fetch-tools.sh        # pinned yt-dlp, ffmpeg and ffprobe, checksum-verified
go tool mygo dev                # run with live reload
go tool mygo build              # build/darwin-arm64/YTGrab.app and a disk image
go test ./...
```

The tools are cached in the ignored `resources/darwin-arm64/bin/`.
Renovate keeps their pins in `scripts/fetch-tools.sh` current.

The app is built on [MyGo](https://mygo.egoist.dev). Its window is MyGo's
native UI, written in Go with no web view.

## Release

1. Bump `version` in `mygo.json` and add a section to `CHANGELOG.md`.
2. Commit, then push a matching tag:

   ```sh
   git tag v2.0.1 && git push origin v2.0.1
   ```

3. The Release workflow tests and builds the app, signs the update, and
   uploads it to a draft release.
4. Publish the draft, and installed apps update to it.

The workflow needs the `MYGO_UPDATER_PRIVATE_KEY` repository secret, holding
the contents of the `mygo-update.key` that `go tool mygo keygen` made. Keep a
copy in a password manager. Installed apps accept only updates signed with
it.

## Usage

Only download content that you own or are authorized to access. You are
responsible for complying with applicable laws and the source site's terms.

## Privacy

YTGrab runs downloads locally and has no analytics or telemetry. URLs, pasted
cookies, time ranges and browser profiles are never saved. Pasted cookies go
to a private temporary file for the length of the download, then are deleted;
any left behind by a crash are removed at the next launch.

Reading cookies from a browser is done by yt-dlp. macOS may ask for Keychain
access, and Chromium-family browsers may need to be quit first. Never share
cookie files or unredacted logs.

## Notices

The bundled yt-dlp and FFmpeg are subject to their own licenses.

YTGrab is an independent project and is not affiliated with YouTube, yt-dlp or
FFmpeg.
