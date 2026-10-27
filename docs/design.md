# YTGrab 2 — design

Status: draft for review · branch `mygo`

## Why

YTGrab 1 is a SwiftUI front end for yt-dlp. Two things make it hard to keep
alive:

- **yt-dlp goes stale.** Sites change every few weeks, and the yt-dlp built into
  the app stops working until someone rebuilds it.
- **The app is hard to build.** It needs a matching Apple toolchain, and a
  mismatched Command Line Tools install already breaks the build.

YTGrab 2 keeps what the app does and changes how it is built and kept current:
a Go app on [MyGo](https://mygo.egoist.dev) whose window is MyGo's native UI,
written in Go with no HTML, JavaScript or webview, and updates delivered from
GitHub Releases.

## Goals

- Works on a Mac with nothing installed: no Homebrew, Python or Terminal.
- Keeps working without a rebuild: yt-dlp updates itself, and the app updates
  itself.
- Builds with only Go: no Xcode, Bun, Node or frontend build.
- Every feature of YTGrab 1, with the same privacy rules.

## Non-goals

- Developer ID signing or notarization. The app is for personal use, so it
  stays ad-hoc signed and the first launch needs a Gatekeeper override.
- Intel Macs, Windows and Linux for now. Nothing in the design rules them out
  later.
- A hosted service, or reimplementing yt-dlp in Go.

## What the app does

1. **Pick what to download.** Paste a URL. Choose video (maximum resolution,
   MP4 or MKV) or audio (MP3, M4A, Opus, FLAC or WAV, with a bitrate for lossy
   formats). Optionally take the whole playlist or only a time range.
2. **Extras.** Embed metadata and cover art, and English subtitles for video.
3. **Sign in when needed.** Either paste a Netscape cookie jar, or read cookies
   from a browser (with an optional profile).
4. **Choose where files go.** The folder is remembered.
5. **Follow the download.** Show the phase (downloading, merging, extracting,
   writing metadata), percent, speed, ETA and title. Allow cancelling. On
   failure, show yt-dlp's last error. Keep the full log viewable and copyable.
   When done, offer Reveal in Finder.
6. **Remember choices.** Format choices and the folder persist. The URL, cookies
   and time range never do.
7. **Keep tools working.** Covered in [Tools](#tools).
8. **Keep the app current.** Covered in [App updates](#app-updates).

## Shape

```
┌──────────────── one Go program ────────────────┐
│ view      form · status footer · log drawer    │
│              │ reads state, calls actions      │
│ jobs      run one yt-dlp download at a time    │
│ tools     find, seed and update yt-dlp         │
│ settings  remembered choices (JSON file)       │
│ desktop   dialogs, clipboard, Finder (MyGo)    │
└──────────────┬─────────────────────────────────┘
               │ child processes
          yt-dlp ──► ffmpeg / ffprobe
```

- **One language, one process.** The window is a MyGo `ui` view, a function
  that builds the interface from the app's state on every frame. There is no
  page, no bridge and no generated client: the view reads Go values and calls
  Go functions.
- **The view only draws and forwards.** It holds the form's values and shows
  job state. Checking a request, building yt-dlp arguments, running the
  process, reading its output, cookies, tools and settings all live in plain
  Go packages with no `ui` import, so they can be unit tested on their own.
- **Background work reports back through `Window.Update`.** The job runner
  and the yt-dlp updater run on goroutines. They change state only inside
  `Window.Update`, which runs on the main thread and redraws the window.
  Slow work (processes, network, the login-shell lookup) never runs in the
  view.

### Packages

```
main ──► view ──► job ──► ytdlp ──► request
           │       ├────► cookies
           │       └────► tools ──► ytdlp
           └────► settings ──► request
```

| Package | Owns | Imports MyGo |
|---|---|---|
| `main` | wiring: paths, menu, window, updater plugin, quit cleanup | yes |
| `internal/view` | the window (`App` state, sections, actions) | `mygo`, `ui` |
| `internal/job` | the yt-dlp process and `State` (status, progress, log) | no |
| `internal/ytdlp` | yt-dlp's CLI: `Args`, `ParseLine`, `ParseUpdate` | no |
| `internal/request` | what the user asked for: `Request`, `Options`, `Validate` | no |
| `internal/cookies` | temporary cookie jars: `Count`, `Normalize`, `Jar` | no |
| `internal/tools` | finding tools, seeding and updating the managed yt-dlp | no |
| `internal/settings` | `settings.json`: `Settings`, `Store` | no |

- Only `main` and `view` know about MyGo, so everything else is plain Go
  and tested with `go test`.
- `request` knows nothing about yt-dlp, and `ytdlp` runs nothing. All
  knowledge of yt-dlp's flags and output sits in `ytdlp`, so swapping in
  go-ytdlp would only touch that package.
- `settings.Settings` holds a `request.Options`, so what is remembered is
  exactly that type, plus the browser, the folder and the yt-dlp fields. Per-download fields (`URL`, `Range`, cookies,
  profile) live only on `request.Request`.

### Jobs

- One job at a time, matching today.
- A request is checked in Go: an http(s) URL, at least one valid timestamp in
  a time range with start before end, and at least one cookie when using
  pasted cookies.
- yt-dlp is called through
  [go-ytdlp](https://github.com/lrstanley/go-ytdlp) for typed flags and
  progress parsing, pointed at our own yt-dlp binary. Its own installer is not
  used, because it pins yt-dlp to the library's version.
- Format rules carry over:
  - A chosen resolution is a hard cap, never falling back above it.
  - MP4 prefers MP4 and M4A streams.
  - No cover art for WAV.
  - A time range cuts at keyframes.
- The process runs in its own process group. Cancel sends SIGTERM to the
  group, so ffmpeg stops too, then SIGKILL after 3 s. Output pipes are closed
  once yt-dlp exits, because ffmpeg can keep them open.
- Job state is one value the view reads:
  - phase, progress, speed, ETA, title
  - error detail
  - produced file
  - the log, capped at the last 2,000 lines

### Cookies

- Pasted cookies go to a `0600` file in a `0700` temporary folder, named with
  the app's process ID.
- The file is deleted when the job ends, is cancelled, or the app quits. Jars
  left by a crash are swept at launch.
- A missing Netscape header is added. `#HttpOnly_` lines count as cookies.
- The log shows `<pasted cookies>` instead of the path.

### Tools

| Tool | Ships in app | Kept current by |
|---|---|---|
| yt-dlp | `resources/darwin-arm64/bin` | daily self-update of a managed copy |
| ffmpeg, ffprobe | `resources/darwin-arm64/bin` | app updates |

- **Fetching at build time.** `scripts/fetch-tools.sh` downloads pinned
  releases and checks their SHA-256 sums. Renovate bumps the pins.
- **The managed copy.** On first launch, the bundled yt-dlp is copied to
  `~/Library/Application Support/YTGrab/bin`. Once a day it runs
  `yt-dlp --update-to stable`, and yt-dlp checks the release's checksums
  itself. The user can also update from the menu, or from a button after a
  failed download.
- **Which yt-dlp is used,** in order:
  1. one the user located
  2. the managed copy
  3. the bundled copy
  4. a copy on `PATH`

  A located copy is never updated by the app.
- **Missing tools.** If neither yt-dlp nor ffmpeg can be found, a banner says
  so and offers Locate….

### App updates

- MyGo's `updater` plugin checks GitHub Releases of `aatng-gh/ytgrab` (public)
  daily and from YTGrab → Check for Updates…. It shows release notes from
  `CHANGELOG.md`, installs, and relaunches.
- Updates are signed with a MyGo update key. The public half is in
  `mygo.config.ts`. The private half lives in the `MYGO_UPDATER_PRIVATE_KEY`
  repository secret and a password manager.
- **Releasing:** bump the version and changelog, then tag `vX.Y.Z`. CI builds
  on macOS, uploads to a draft release, and you publish it.
- The app must live in `/Applications` or `~/Applications` to update itself.

### Settings

- Stored as JSON in `~/Library/Application Support/YTGrab/settings.json`:
  - format choices and toggles
  - output folder
  - browser
  - located yt-dlp path
  - time of the last yt-dlp check
- Never stored: URL, cookies, time range, browser profile.

### Window

- One window drawn by MyGo's `ui` package:
  - the form in four sections (Source, Format, Sign-in, Destination)
  - a status footer with progress, Cancel, Log and Reveal in Finder
  - the log as a collapsible drawer in the same window, with Copy
- Controls come from MyGo: text inputs, selects, segmented controls,
  checkboxes, a progress bar and buttons. They follow the system's light and
  dark mode and accent color.
- The form is disabled while a job runs.
- Menu: YTGrab (About, Check for Updates…, Update yt-dlp, Quit), Edit, Window.
  The update window is MyGo's native one (`updater/native`), so no webview
  is ever started.

## Build and CI

- Configuration is `mygo.json`, which needs no JavaScript runtime.
- `go tool mygo dev`: run with live reload. `scripts/fetch-tools.sh` runs
  first if the tools are missing.
- `go tool mygo build`: produces `build/darwin-arm64/YTGrab.app` and a disk
  image.
- **CI on pull requests:** `go test`, `go tool mygo vet` and a build.
- **CI on tags:** a release build signed with the update key, uploaded to a
  draft release.

## Testing

- **Go unit tests:**
  - request checks and timestamps
  - argument building (resolution caps, WAV art, cookies, time ranges)
  - cookie parsing and normalising
  - reading yt-dlp's progress, file and error lines
  - reading yt-dlp's update output
- **View tests** with `ui.NewTester`, which runs the view without a window
  and clicks and types as a user would:
  - the Download button enables only for a valid request
  - switching Video/Audio and sign-in modes shows the right fields
  - the form locks while a job runs, and Cancel is offered
- **Manual smoke test per release:**
  - one video, one audio extract, one cancel
  - a pasted-cookie download
  - an app update from the previous release

## Risks and open questions

- **Ad-hoc self-update is unproven.** MyGo's docs don't say whether an
  ad-hoc-signed app can update itself. This is tested first, with two
  throwaway prereleases.
- **First launch needs a Gatekeeper override.** A downloaded ad-hoc app needs
  System Settings → Open Anyway once. Updates should not, because the app
  downloads them itself and they carry no quarantine flag. To be confirmed in
  the same test.
- **MyGo is at 0.7.1, and its `ui` package changes fastest.** Pin it and
  upgrade on purpose, using `go tool mygo migrate-ui` for `ui` changes.
- **It won't look like a Mac app.** MyGo draws its own controls instead of
  AppKit's, so the window looks like a MyGo app. That's the accepted cost of
  an all-Go interface.
- **go-ytdlp vs writing our own.** If go-ytdlp's flags or progress handling
  get in the way, building the argument list ourselves is about 60 lines and
  removes a dependency.

## Milestones

1. **Prove the pipeline.**
   - Skeleton native-UI app, bundled tools and one real download with live
     progress.
   - Release `v2.0.0-alpha.1` and `alpha.2`.
   - Confirm that alpha.1 updates itself to alpha.2.
2. **Feature parity** with YTGrab 1, plus the tests above.
3. **Ship.** Docs, Renovate rules for the new pins, and `v2.0.0`. Merge
   `mygo` into `main`.
