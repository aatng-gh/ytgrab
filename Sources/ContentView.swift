import SwiftUI
import AppKit
import UniformTypeIdentifiers

struct ContentView: View {
    @EnvironmentObject private var downloader: Downloader

    @State private var request = DownloadRequest()
    @State private var cookieLoadError = ""

    @Environment(\.openWindow) private var openWindow

    @AppStorage("ytdlpPath") private var ytdlpOverride = ""
    @AppStorage("outputDirectory") private var storedOutputDirectory = ""

    @State private var ytdlp: String?
    @State private var ffmpeg: String?
    /// Discovery is async, so don't flash "not found" before it has answered.
    @State private var toolsChecked = false

    var body: some View {
        VStack(spacing: 0) {
            if toolsChecked && (ytdlp == nil || ffmpeg == nil) { missingToolBanner }

            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    sourceSection
                    Divider().padding(.vertical, 8)
                    formatSection
                    Divider().padding(.vertical, 8)
                    authSection
                    Divider().padding(.vertical, 8)
                    destinationSection
                }
                .padding(12)
            }
            .scrollBounceBehavior(.basedOnSize)

            Divider()
            footer
        }
        // No maxWidth/maxHeight here: capping the *content* while the window
        // stays resizable just leaves empty gutters. `.windowResizability`
        // (App.swift) ties the window to this content size instead.
        .frame(
            minWidth: 500,
            idealWidth: 520,
            minHeight: request.trimEnabled ? 650 : 520,
            idealHeight: request.trimEnabled ? 680 : 620
        )
        .onAppear {
            request.loadPreferences()
            locateTools()
        }
        .onChange(of: request) { request.savePreferences() }
    }

    // MARK: - Section scaffolding

    /// A label-left / control-right row, the way System Settings lays out a form.
    private func row(_ label: String, @ViewBuilder content: () -> some View) -> some View {
        HStack(spacing: 12) {
            Text(label)
                .font(.callout)
                .foregroundStyle(.secondary)
                .frame(width: 88, alignment: .leading)
            content()
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// A topic and its rows, sitting straight on the window's own background —
    /// dividers between sections do the separating, no boxes.
    private func section(_ title: String, icon: String, @ViewBuilder content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: icon).foregroundStyle(.tint)
                Text(title).foregroundStyle(.primary)
            }
            .font(.subheadline.weight(.semibold))
            VStack(alignment: .leading, spacing: 6) { content() }
        }
    }

    // MARK: - Sections

    private var sourceSection: some View {
        section("Source", icon: "play.rectangle") {
            HStack(spacing: 8) {
                ZStack(alignment: .leading) {
                    if request.url.isEmpty {
                        Text("https://www.youtube.com/watch?v=…")
                            .foregroundStyle(.secondary)
                            .allowsHitTesting(false)
                    }
                    TextField("", text: $request.url)
                        .textFieldStyle(.plain)
                        .foregroundStyle(.primary)
                }
                .padding(.horizontal, 8).padding(.vertical, 5)
                .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(.separator))
                .disabled(downloader.isRunning)
                Button { pasteURL() } label: {
                    Image(systemName: "doc.on.clipboard")
                }
                .buttonStyle(.bordered)
                .foregroundStyle(.primary)
                .help("Paste from clipboard")
                .disabled(downloader.isRunning)
            }
            Toggle("Download the whole playlist if the link is one", isOn: $request.downloadPlaylist)
                .disabled(downloader.isRunning)
            Toggle("Download only a time range", isOn: $request.trimEnabled.animation())
                .disabled(downloader.isRunning)
            if request.trimEnabled {
                HStack(spacing: 8) {
                    timeField("Start", placeholder: "0:00", text: $request.trimStart)
                    Text("–").foregroundStyle(.secondary)
                        .padding(.top, 18)
                    timeField("End", placeholder: "To end", text: $request.trimEnd)
                }
                Text(request.trimIsValid
                     ? "h:mm:ss or m:ss — leave one blank for \"to the end\" or \"from the start\"."
                     : "Enter at least one valid timestamp, like 1:30 or 12:04:00.")
                    .font(.caption)
                    .foregroundStyle(request.trimIsValid ? .secondary : Color.orange)
            }
        }
        .disabled(downloader.isRunning)
    }

    private func timeField(_ label: String, placeholder: String, text: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            TextField(placeholder, text: text)
                .textFieldStyle(.roundedBorder)
                .font(.system(.callout, design: .monospaced))
        }
        .frame(width: 112)
        .disabled(downloader.isRunning)
    }

    private var formatSection: some View {
        section("Format", icon: "slider.horizontal.3") {
            Picker("", selection: $request.kind) {
                ForEach(MediaKind.allCases) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            .labelsHidden()

            if request.kind == .video {
                row("Quality") {
                    Picker("", selection: $request.videoQuality) {
                        ForEach(VideoQuality.allCases) { Text($0.label).tag($0) }
                    }.labelsHidden()
                }
                row("Container") {
                    Picker("", selection: $request.container) {
                        ForEach(Container.allCases) { Text($0.label).tag($0) }
                    }.labelsHidden()
                }
            } else {
                row("Format") {
                    Picker("", selection: $request.audioFormat) {
                        ForEach(AudioFormat.allCases) { Text($0.label).tag($0) }
                    }.labelsHidden()
                }
                row("Quality") {
                    Picker("", selection: $request.audioQuality) {
                        ForEach(AudioQuality.allCases) { Text($0.label).tag($0) }
                    }
                    .labelsHidden()
                    .disabled(request.audioFormat.isLossless)
                }
                if request.audioFormat.isLossless {
                    Text("Lossless formats keep whatever the source provides.")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .disabled(downloader.isRunning)
    }

    private var authSection: some View {
        section("Sign-in", icon: "key") {
            Picker("", selection: $request.authMode) {
                ForEach(AuthMode.allCases) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()

            switch request.authMode {
            case .none:
                Text("Public videos only.")
                    .font(.caption).foregroundStyle(.secondary)

            case .pastedCookies:
                TextField("Paste Netscape cookies.txt contents", text: $request.cookieText, axis: .vertical)
                    .font(.system(.caption, design: .monospaced))
                    .lineLimit(3...5)
                    .textFieldStyle(.roundedBorder)
                HStack {
                    Button("Paste") { pasteCookies() }
                        .buttonStyle(.bordered).foregroundStyle(.primary)
                    Button("Load file…") { loadCookieFile() }
                        .buttonStyle(.bordered).foregroundStyle(.primary)
                    Button("Clear") { request.cookieText = "" }
                        .buttonStyle(.bordered)
                        .foregroundStyle(.primary)
                        .disabled(request.cookieText.isEmpty)
                    Spacer()
                    HStack(spacing: 4) {
                        if cookieCount == 0 && !request.cookieText.isEmpty {
                            Image(systemName: "exclamationmark.triangle.fill")
                                .foregroundStyle(.orange)
                                .imageScale(.small)
                        }
                        Text(cookieSummary)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Text("Netscape cookies only. Stored in a private temporary file for the download, then deleted.")
                    .font(.caption).foregroundStyle(.secondary)
                if !cookieLoadError.isEmpty {
                    Text(cookieLoadError)
                        .font(.caption).foregroundStyle(.orange)
                }

            case .browser:
                row("Browser") {
                    Picker("", selection: $request.browser) {
                        ForEach(BrowserChoice.allCases) { Text($0.label).tag($0) }
                    }.labelsHidden()
                }
                row("Profile") {
                    ZStack(alignment: .leading) {
                        if request.browserProfile.isEmpty {
                            Text("Optional")
                                .foregroundStyle(.secondary)
                                .allowsHitTesting(false)
                        }
                        TextField("", text: $request.browserProfile)
                            .textFieldStyle(.plain)
                            .foregroundStyle(.primary)
                    }
                    .padding(.horizontal, 8).padding(.vertical, 5)
                    .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(.separator))
                }
                Text("Reads cookies straight from the browser. Chromium browsers must be quit "
                     + "first, and macOS will ask for Keychain access.")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .disabled(downloader.isRunning)
    }

    private var destinationSection: some View {
        section("Destination", icon: "folder") {
            HStack {
                Text(request.outputDir.path)
                    .font(.callout)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .help(request.outputDir.path)
                Spacer()
                Button("Choose…") { chooseOutputDirectory() }
                    .buttonStyle(.bordered)
                    .foregroundStyle(.primary)
                    .disabled(downloader.isRunning)
            }
            Toggle("Embed metadata and cover art", isOn: $request.embedExtras)
                .disabled(downloader.isRunning)
            if request.kind == .video {
                Toggle("Include English subtitles", isOn: $request.subtitles)
                    .disabled(downloader.isRunning)
            }
        }
    }

    // MARK: - Footer

    private var footer: some View {
        VStack(spacing: 8) {
            HStack(spacing: 8) {
                statusIcon
                VStack(alignment: .leading, spacing: 2) {
                    Text(downloader.status)
                        .font(.callout)
                        .foregroundStyle(downloader.failed ? Color.red : .primary)
                    if downloader.failed && !downloader.errorDetail.isEmpty {
                        Text(downloader.errorDetail)
                            .font(.caption).foregroundStyle(.secondary)
                            .lineLimit(2).truncationMode(.tail)
                            .help(downloader.errorDetail)
                    } else if !downloader.title.isEmpty {
                        Text(downloader.title)
                            .font(.caption).foregroundStyle(.secondary)
                            .lineLimit(1).truncationMode(.middle)
                    }
                }
                Spacer()
                if !downloader.speed.isEmpty || !downloader.eta.isEmpty {
                    Text([downloader.speed, downloader.eta.isEmpty ? "" : "ETA \(downloader.eta)"]
                        .filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                }
            }

            if downloader.isRunning {
                if downloader.progress >= 0 {
                    ProgressView(value: downloader.progress)
                } else {
                    ProgressView().progressViewStyle(.linear)
                }
            }

            HStack {
                Button("Log") { openWindow(id: "log") }
                    .buttonStyle(.bordered)
                    .foregroundStyle(.primary)
                    .help("Show yt-dlp output")
                if let file = downloader.producedFile, !downloader.isRunning {
                    Button("Reveal in Finder") {
                        NSWorkspace.shared.activateFileViewerSelecting([file])
                    }
                    .buttonStyle(.bordered)
                    .foregroundStyle(.primary)
                }
                Spacer()
                if downloader.isRunning {
                    Button("Cancel", role: .cancel) { downloader.cancel() }
                        .buttonStyle(.bordered)
                        .foregroundStyle(.primary)
                        .keyboardShortcut(".", modifiers: .command)
                        .disabled(downloader.cancelling)
                } else {
                    Button("Download") { startDownload() }
                        .buttonStyle(.borderedProminent)
                        .keyboardShortcut(.defaultAction)
                        .disabled(!request.isReady || ytdlp == nil || ffmpeg == nil)
                }
            }
        }
        .padding(10)
    }

    @ViewBuilder private var statusIcon: some View {
        if downloader.isRunning {
            Image(systemName: "arrow.down.circle").foregroundStyle(.tint)
        } else if downloader.failed {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
        } else if downloader.producedFile != nil {
            Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
        } else {
            Image(systemName: "circle.dashed").foregroundStyle(.secondary)
        }
    }

    private var missingToolBanner: some View {
        HStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 2) {
                Text(missingToolTitle).font(.callout.weight(.medium))
                Text("Install both with `brew install yt-dlp ffmpeg`, or locate yt-dlp manually.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if ytdlp == nil {
                Button("Locate…") { locateYtdlpManually() }
                    .buttonStyle(.bordered)
                    .foregroundStyle(.primary)
            }
        }
        .padding(8)
        .frame(maxWidth: .infinity)
        .background(Color.orange.opacity(0.12))
    }

    // MARK: - Derived values

    private var cookieCount: Int { Cookies.parsedCount(in: request.cookieText) }

    private var missingToolTitle: String {
        if ytdlp == nil && ffmpeg == nil { return "yt-dlp and FFmpeg not found" }
        return ytdlp == nil ? "yt-dlp not found" : "FFmpeg not found"
    }

    private var cookieSummary: String {
        request.cookieText.isEmpty ? "nothing pasted yet"
            : cookieCount == 0 ? "no cookies recognised"
            : "\(cookieCount) cookie\(cookieCount == 1 ? "" : "s")"
    }

    // MARK: - Actions

    /// Tool discovery can end up asking the user's login shell, which sources
    /// their profile — nvm/conda/pyenv init can take seconds. Keep it off the
    /// main actor so a slow profile never beachballs the window.
    private func locateTools() {
        if !storedOutputDirectory.isEmpty {
            let dir = URL(fileURLWithPath: storedOutputDirectory)
            if FileManager.default.fileExists(atPath: dir.path) { request.outputDir = dir }
        }
        let override = ytdlpOverride
        Task {
            let found = await Task.detached(priority: .userInitiated) {
                (ToolLocator.find("yt-dlp", override: override), ToolLocator.find("ffmpeg"))
            }.value
            ytdlp = found.0
            ffmpeg = found.1
            toolsChecked = true
        }
    }

    private func startDownload() {
        guard let ytdlp else { return }
        downloader.start(request, ytdlp: ytdlp, ffmpeg: ffmpeg)
    }

    private func pasteURL() {
        if let text = NSPasteboard.general.string(forType: .string) {
            request.url = text.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

    private func pasteCookies() {
        if let text = NSPasteboard.general.string(forType: .string) {
            request.cookieText = text
        }
    }

    private func loadCookieFile() {
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = false
        panel.canChooseDirectories = false
        panel.allowedContentTypes = [.plainText, .text, .data]
        panel.message = "Choose a cookies.txt export"
        guard panel.runModal() == .OK, let url = panel.url else { return }

        cookieLoadError = ""
        // Cookie jars are usually UTF-8, but fall back to whatever the file
        // actually is rather than silently doing nothing.
        if let text = try? String(contentsOf: url, encoding: .utf8) {
            request.cookieText = text
            return
        }
        var used = String.Encoding.utf8
        if let text = try? String(contentsOf: url, usedEncoding: &used) {
            request.cookieText = text
        } else {
            cookieLoadError = "Could not read \(url.lastPathComponent) — is it a text file?"
        }
    }

    private func chooseOutputDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.canCreateDirectories = true
        panel.directoryURL = request.outputDir
        panel.prompt = "Choose"
        if panel.runModal() == .OK, let url = panel.url {
            request.outputDir = url
            storedOutputDirectory = url.path
        }
    }

    private func locateYtdlpManually() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.showsHiddenFiles = true
        panel.directoryURL = URL(fileURLWithPath: "/opt/homebrew/bin")
        panel.message = "Choose the yt-dlp executable"
        if panel.runModal() == .OK, let url = panel.url {
            ytdlpOverride = url.path
            locateTools()
        }
    }
}
