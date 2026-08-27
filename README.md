<p align="center">
  <img src="Resources/AppIcon.png" width="128" height="128" alt="YTGrab icon">
</p>

# YTGrab

YTGrab is a small native macOS front end for [yt-dlp](https://github.com/yt-dlp/yt-dlp). It provides the common download controls without requiring users to assemble command-line arguments themselves.

## Features

- Download video or extract audio
- Select a maximum video resolution and MP4 or MKV output
- Export MP3, M4A, Opus, FLAC, or WAV audio
- Download playlists or a selected time range
- Embed metadata, cover art, and English subtitles
- Access signed-in content with a temporary Netscape cookie jar or browser cookies
- View live progress and the complete yt-dlp log
- Cancel downloads and reveal completed files in Finder

## Requirements

- macOS 14 or newer
- Apple Silicon for the self-contained app bundle
- Apple Command Line Tools or Xcode
- An internet connection during the first build

The build downloads pinned yt-dlp, FFmpeg, and ffprobe executables, verifies their SHA-256 checksums, and embeds them in the application. The downloaded tools are cached in the ignored `Resources/vendor/` directory.

## Build

Install the Apple Command Line Tools if necessary:

```sh
xcode-select --install
```

Then build and open YTGrab:

```sh
./build.sh
open build/YTGrab.app
```

The resulting app is ad-hoc signed for local use. It is not Developer ID signed or notarized for third-party distribution.

## Usage

1. Paste a URL supported by yt-dlp.
2. Choose video or audio settings.
3. Select an authentication method only if the content requires it.
4. Choose a destination and click **Download**.

Only download content that you own or are authorized to access. You are responsible for complying with applicable laws and the source site's terms.

## Privacy

YTGrab runs downloads locally and does not include analytics or telemetry. URLs, pasted cookies, and trim ranges are not saved in preferences. Pasted cookies are written to a private temporary file for the active download and removed afterward; stale temporary jars are cleaned up when the app launches.

Using browser authentication delegates cookie access to yt-dlp. macOS may request Keychain permission, and Chromium-family browsers may need to be closed first. Never share cookie files or unredacted verbose logs.

## Development

Run the logic tests with:

```sh
./Tests/run.sh
```

CI runs both the logic tests and a clean app build on macOS.

## License

YTGrab is available under the [MIT License](LICENSE). The downloaded yt-dlp and FFmpeg components remain subject to their respective licenses.

YTGrab is an independent project and is not affiliated with YouTube, yt-dlp, or FFmpeg.
