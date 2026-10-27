import Foundation

// Models.swift uses this marker while building its progress arguments. The
// production Downloader is intentionally excluded from these pure logic tests.
enum Downloader {
    static let progressPrefix = "@@PROG@@"
}

@main
struct LogicTests {
    private static var failures = 0

    static func main() throws {
        check(DownloadRequest.isValidTimestamp("1:30"), "accepts m:ss")
        check(DownloadRequest.isValidTimestamp("12:04:00"), "accepts h:mm:ss")
        check(!DownloadRequest.isValidTimestamp("1:2"), "rejects incomplete seconds")

        var capped = DownloadRequest()
        capped.videoQuality = .p360
        let cappedSelector = ArgumentBuilder.videoFormatSelector(capped)
        check(cappedSelector.contains("height<=360"), "applies selected height cap")
        check(!cappedSelector.contains("bv*+ba"), "does not fall back above selected cap")

        capped.videoQuality = .best
        check(ArgumentBuilder.videoFormatSelector(capped).contains("bv*+ba"),
              "keeps uncapped fallback for Best available")

        let cookie = ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tvalue"
        check(Cookies.parsedCount(in: cookie) == 1, "recognizes Netscape cookies")
        check(Cookies.normalized(cookie).hasPrefix(Cookies.netscapeHeader + "\n"),
              "adds a missing Netscape header")

        let jar = try Cookies.writeTemporaryJar(cookie)
        check(jar != nil, "creates a temporary cookie jar")
        if let jar {
            let pidToken = "YTGrab-\(ProcessInfo.processInfo.processIdentifier)-"
            check(jar.deletingLastPathComponent().lastPathComponent.hasPrefix(pidToken),
                  "records the owner process in the jar directory")
            Cookies.shred(jar)
            check(!FileManager.default.fileExists(atPath: jar.path), "removes the cookie jar")
        }

        let updated = YtdlpUpdater.summarize(output: """
            Current version: stable@2026.08.19 from yt-dlp/yt-dlp
            Latest version: stable@2026.10.26 from yt-dlp/yt-dlp
            Updating to stable@2026.10.26 from yt-dlp/yt-dlp ...
            Updated yt-dlp to stable@2026.10.26 from yt-dlp/yt-dlp
            """, exitCode: 0)
        check(updated.ok && updated.message == "Updated yt-dlp to stable@2026.10.26 from yt-dlp/yt-dlp",
              "reports a completed yt-dlp update")

        let current = YtdlpUpdater.summarize(output: """
            Latest version: stable@2026.10.26 from yt-dlp/yt-dlp
            yt-dlp is up to date (stable@2026.10.26 from yt-dlp/yt-dlp)
            """, exitCode: 0)
        check(current.ok && current.message.hasPrefix("yt-dlp is up to date"),
              "reports an up-to-date yt-dlp")

        let offline = YtdlpUpdater.summarize(output: """
            ERROR: Unable to obtain version info (<urlopen error [Errno 8] nodename nor servname provided>)
            """, exitCode: 1)
        check(!offline.ok && offline.message.hasPrefix("yt-dlp update failed: Unable to obtain version info"),
              "surfaces the updater's error")
        check(!YtdlpUpdater.summarize(output: "", exitCode: 1).ok, "treats silent failure as failure")

        if failures > 0 {
            throw TestFailure(count: failures)
        }
        print("All logic tests passed")
    }

    private static func check(_ condition: @autoclosure () -> Bool, _ message: String) {
        if condition() {
            print("PASS: \(message)")
        } else {
            failures += 1
            print("FAIL: \(message)")
        }
    }

    private struct TestFailure: Error {
        let count: Int
    }
}
