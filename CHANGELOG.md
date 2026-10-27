# Changelog

## 2.0.1

- Switching between video and audio, or between sign-in modes, no longer
  shifts the window's controls sideways.

## 2.0.0

- Rebuilt as a Go app on MyGo. It builds with only Go, and needs nothing
  installed to run.
- yt-dlp updates itself once a day, from the YTGrab menu, or after a failed
  download, so the app keeps working without a rebuild.
- YTGrab updates itself from GitHub Releases (YTGrab → Check for Updates…).
- Cancel now also stops FFmpeg.
