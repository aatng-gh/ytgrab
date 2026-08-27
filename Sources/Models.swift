import Foundation

// MARK: - Choices exposed in the UI

enum MediaKind: String, CaseIterable, Identifiable {
    case video, audio
    var id: String { rawValue }
    var label: String { self == .video ? "Video" : "Audio only" }
}

enum VideoQuality: String, CaseIterable, Identifiable {
    case best, p2160, p1440, p1080, p720, p480, p360
    var id: String { rawValue }

    var label: String {
        switch self {
        case .best:  return "Best available"
        case .p2160: return "2160p (4K)"
        case .p1440: return "1440p (2K)"
        case .p1080: return "1080p"
        case .p720:  return "720p"
        case .p480:  return "480p"
        case .p360:  return "360p"
        }
    }

    /// nil means "no cap".
    var maxHeight: Int? {
        switch self {
        case .best:  return nil
        case .p2160: return 2160
        case .p1440: return 1440
        case .p1080: return 1080
        case .p720:  return 720
        case .p480:  return 480
        case .p360:  return 360
        }
    }
}

enum Container: String, CaseIterable, Identifiable {
    case mp4, mkv
    var id: String { rawValue }
    var label: String { self == .mp4 ? "MP4 (most compatible)" : "MKV (never re-encodes)" }
}

enum AudioFormat: String, CaseIterable, Identifiable {
    case mp3, m4a, opus, flac, wav
    var id: String { rawValue }
    var label: String {
        switch self {
        case .mp3:  return "MP3"
        case .m4a:  return "M4A (AAC)"
        case .opus: return "Opus"
        case .flac: return "FLAC (lossless)"
        case .wav:  return "WAV (lossless)"
        }
    }
    var isLossless: Bool { self == .flac || self == .wav }

    /// yt-dlp's thumbnail post-processor supports mp3, m4a/mp4, opus/ogg, flac
    /// and mkv — WAV has no standard cover-art container, and asking for one
    /// makes the whole run fail *after* the download has already finished.
    var supportsThumbnailEmbedding: Bool { self != .wav }
}

enum AudioQuality: String, CaseIterable, Identifiable {
    case best, k320, k256, k192, k128
    var id: String { rawValue }
    var label: String {
        switch self {
        case .best: return "Best available"
        case .k320: return "320 kbps"
        case .k256: return "256 kbps"
        case .k192: return "192 kbps"
        case .k128: return "128 kbps"
        }
    }
    /// Value for `--audio-quality`.
    var ytdlpValue: String {
        switch self {
        case .best: return "0"
        case .k320: return "320K"
        case .k256: return "256K"
        case .k192: return "192K"
        case .k128: return "128K"
        }
    }
}

enum AuthMode: String, CaseIterable, Identifiable {
    case none, pastedCookies, browser
    var id: String { rawValue }
    var label: String {
        switch self {
        case .none:          return "None"
        case .pastedCookies: return "Paste cookies"
        case .browser:       return "From browser"
        }
    }
}

enum BrowserChoice: String, CaseIterable, Identifiable {
    case chrome, safari, firefox, edge, brave, chromium, opera, vivaldi
    var id: String { rawValue }
    var label: String {
        switch self {
        case .chrome:   return "Chrome"
        case .safari:   return "Safari"
        case .firefox:  return "Firefox"
        case .edge:     return "Edge"
        case .brave:    return "Brave"
        case .chromium: return "Chromium"
        case .opera:    return "Opera"
        case .vivaldi:  return "Vivaldi"
        }
    }
}

// MARK: - A single download job

struct DownloadRequest: Equatable {
    var url: String = ""
    var kind: MediaKind = .video
    var videoQuality: VideoQuality = .p1080
    var container: Container = .mp4
    var audioFormat: AudioFormat = .m4a
    var audioQuality: AudioQuality = .best
    var authMode: AuthMode = .none
    var cookieText: String = ""
    var browser: BrowserChoice = .chrome
    var browserProfile: String = ""
    var outputDir: URL = FileManager.default
        .urls(for: .downloadsDirectory, in: .userDomainMask).first
        ?? URL(fileURLWithPath: NSHomeDirectory())
    var downloadPlaylist: Bool = false
    var embedExtras: Bool = true
    var subtitles: Bool = false
    var trimEnabled: Bool = false
    var trimStart: String = ""
    var trimEnd: String = ""

    var trimmedURL: String { url.trimmingCharacters(in: .whitespacesAndNewlines) }
    var trimmedStart: String { trimStart.trimmingCharacters(in: .whitespaces) }
    var trimmedEnd: String { trimEnd.trimmingCharacters(in: .whitespaces) }

    /// H:MM:SS, M:SS, or plain seconds, with optional fractional seconds and an
    /// optional leading minus (yt-dlp counts a negative offset from the end of
    /// the video). The leading component is unbounded: "300" is a legitimate
    /// five-minute mark, and capping it at two digits used to reject it.
    static func isValidTimestamp(_ s: String) -> Bool {
        s.range(of: #"^-?\d+(:\d{2}){0,2}(\.\d+)?$"#, options: .regularExpression) != nil
    }

    /// Seconds represented by a validated timestamp; nil if it is relative to
    /// the end of the video, which we cannot resolve without knowing duration.
    static func seconds(from s: String) -> Double? {
        guard !s.hasPrefix("-") else { return nil }
        let parts = s.split(separator: ":").map(String.init)
        guard !parts.isEmpty, parts.count <= 3 else { return nil }
        return parts.reduce(0) { total, part in total * 60 + (Double(part) ?? 0) }
    }

    /// True once the range has at least one valid, usable endpoint.
    var trimIsValid: Bool {
        guard trimEnabled else { return true }
        let start = trimmedStart, end = trimmedEnd
        guard !start.isEmpty || !end.isEmpty else { return false }
        if !start.isEmpty && !Self.isValidTimestamp(start) { return false }
        if !end.isEmpty && !Self.isValidTimestamp(end) { return false }
        // Both absolute: an inverted range yields no output, and yt-dlp accepts
        // it silently, so catch it here rather than producing an empty file.
        if let s = Self.seconds(from: start), let e = Self.seconds(from: end), s >= e {
            return false
        }
        return true
    }

    var isReady: Bool {
        let u = trimmedURL
        guard !u.isEmpty else { return false }
        guard u.hasPrefix("http://") || u.hasPrefix("https://") else { return false }
        if authMode == .pastedCookies && Cookies.parsedCount(in: cookieText) == 0 { return false }
        if !trimIsValid { return false }
        return true
    }
}

// MARK: - Remembering the user's format choices

extension DownloadRequest {

    private enum Key {
        static let kind = "kind"
        static let videoQuality = "videoQuality"
        static let container = "container"
        static let audioFormat = "audioFormat"
        static let audioQuality = "audioQuality"
        static let embedExtras = "embedExtras"
        static let subtitles = "subtitles"
        static let playlist = "downloadPlaylist"
        static let browser = "browser"
    }

    /// Deliberately excludes the URL, the cookie text, and the trim range —
    /// those are per-download, and the cookies are credentials we never persist.
    func savePreferences() {
        let d = UserDefaults.standard
        d.set(kind.rawValue, forKey: Key.kind)
        d.set(videoQuality.rawValue, forKey: Key.videoQuality)
        d.set(container.rawValue, forKey: Key.container)
        d.set(audioFormat.rawValue, forKey: Key.audioFormat)
        d.set(audioQuality.rawValue, forKey: Key.audioQuality)
        d.set(embedExtras, forKey: Key.embedExtras)
        d.set(subtitles, forKey: Key.subtitles)
        d.set(downloadPlaylist, forKey: Key.playlist)
        d.set(browser.rawValue, forKey: Key.browser)
    }

    mutating func loadPreferences() {
        let d = UserDefaults.standard
        if let v = d.string(forKey: Key.kind), let k = MediaKind(rawValue: v) { kind = k }
        if let v = d.string(forKey: Key.videoQuality), let q = VideoQuality(rawValue: v) { videoQuality = q }
        if let v = d.string(forKey: Key.container), let c = Container(rawValue: v) { container = c }
        if let v = d.string(forKey: Key.audioFormat), let f = AudioFormat(rawValue: v) { audioFormat = f }
        if let v = d.string(forKey: Key.audioQuality), let q = AudioQuality(rawValue: v) { audioQuality = q }
        if let v = d.string(forKey: Key.browser), let b = BrowserChoice(rawValue: v) { browser = b }
        if d.object(forKey: Key.embedExtras) != nil { embedExtras = d.bool(forKey: Key.embedExtras) }
        if d.object(forKey: Key.subtitles) != nil { subtitles = d.bool(forKey: Key.subtitles) }
        if d.object(forKey: Key.playlist) != nil { downloadPlaylist = d.bool(forKey: Key.playlist) }
    }
}

// MARK: - Argument construction (pure, so the UI can preview it)

enum ArgumentBuilder {

    /// Builds the full yt-dlp argument list. `cookieFile` is the temp file the
    /// pasted cookie text was written to, if any.
    static func arguments(for req: DownloadRequest,
                          cookieFile: URL?,
                          ffmpegDirectory: String?) -> [String] {
        var args: [String] = []

        // Machine-readable progress on its own lines.
        args += ["--newline",
                 "--progress-template",
                 "download:\(Downloader.progressPrefix)%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s|%(info.title)s"]

        args += req.downloadPlaylist ? ["--yes-playlist"] : ["--no-playlist"]
        args += ["--concurrent-fragments", "4"]

        // Where things land.
        args += ["-P", req.outputDir.path]
        args += ["-o", req.downloadPlaylist
                 ? "%(playlist_title)s/%(playlist_index)s - %(title)s [%(id)s].%(ext)s"
                 : "%(title)s [%(id)s].%(ext)s"]

        if let dir = ffmpegDirectory {
            args += ["--ffmpeg-location", dir]
        }

        // Format selection.
        switch req.kind {
        case .video:
            args += ["-f", videoFormatSelector(req)]
            args += ["--merge-output-format", req.container.rawValue]
            if req.embedExtras { args += ["--embed-metadata", "--embed-thumbnail"] }
            if req.subtitles {
                args += ["--write-subs", "--write-auto-subs", "--sub-langs", "en.*,en", "--embed-subs"]
            }
        case .audio:
            args += ["-f", "ba/b"]
            args += ["-x", "--audio-format", req.audioFormat.rawValue]
            if !req.audioFormat.isLossless {
                args += ["--audio-quality", req.audioQuality.ytdlpValue]
            }
            if req.embedExtras {
                args += ["--embed-metadata"]
                if req.audioFormat.supportsThumbnailEmbedding { args += ["--embed-thumbnail"] }
            }
        }

        // Trim to a time range, if one was given.
        if req.trimEnabled, req.trimIsValid, !req.trimmedStart.isEmpty || !req.trimmedEnd.isEmpty {
            args += ["--download-sections", "*\(req.trimmedStart)-\(req.trimmedEnd)"]
            args += ["--force-keyframes-at-cuts"]
        }

        // Auth.
        switch req.authMode {
        case .none:
            break
        case .pastedCookies:
            if let cookieFile { args += ["--cookies", cookieFile.path] }
        case .browser:
            let profile = req.browserProfile.trimmingCharacters(in: .whitespaces)
            args += ["--cookies-from-browser",
                     profile.isEmpty ? req.browser.rawValue : "\(req.browser.rawValue):\(profile)"]
        }

        args.append(req.trimmedURL)
        return args
    }

    static func videoFormatSelector(_ req: DownloadRequest) -> String {
        let preferMP4 = req.container == .mp4
        if let h = req.videoQuality.maxHeight {
            var chain: [String] = []
            if preferMP4 { chain.append("bv*[height<=\(h)][ext=mp4]+ba[ext=m4a]") }
            // A selected quality is a cap. Falling back to an uncapped format
            // makes a 360p request silently download 4K on sparse sites.
            chain += ["bv*[height<=\(h)]+ba", "b[height<=\(h)]"]
            return chain.joined(separator: "/")
        } else {
            var chain: [String] = []
            if preferMP4 { chain.append("bv*[ext=mp4]+ba[ext=m4a]") }
            chain += ["bv*+ba", "b"]
            return chain.joined(separator: "/")
        }
    }

    /// Shell-ish rendering used only for the on-screen command preview.
    static func preview(tool: String, args: [String]) -> String {
        ([tool] + args).map { arg in
            let safe = CharacterSet(charactersIn:
                "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@%+,")
            return arg.unicodeScalars.allSatisfy(safe.contains) && !arg.isEmpty
                ? arg
                : "'" + arg.replacingOccurrences(of: "'", with: "'\\''") + "'"
        }.joined(separator: " ")
    }
}
